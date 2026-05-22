from __future__ import annotations

import argparse
import json
import mimetypes
import time
import sys
import threading
import traceback
import webbrowser
from datetime import datetime, time as dt_time, timedelta
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from ssq_predictor import build_report
from ssq_predictor.db import get_latest_draw
from ssq_predictor.service import (
    configure_runtime,
    get_schedule_description,
    RETRY_HOUR,
    RETRY_MINUTE,
    SCHEDULE_HOUR,
    SCHEDULE_MINUTE,
    SCHEDULE_WEEKDAYS,
    sync_history,
)

BASE_DIR = Path(__file__).resolve().parent
STATIC_DIR = BASE_DIR / "static"
PAGE_ROUTES = {
    "/",
    "/dashboard",
    "/prediction",
    "/backtest",
    "/stats",
    "/history",
    "/notes",
}


class AutoSyncWorker(threading.Thread):
    def __init__(self) -> None:
        super().__init__(daemon=True)
        self._stop_event = threading.Event()
        self._pending_retry: dict[str, str] | None = None

    def _next_regular_sync_dt(self, now: datetime) -> datetime:
        for offset in range(0, 8):
            candidate_date = now.date() + timedelta(days=offset)
            if candidate_date.weekday() not in SCHEDULE_WEEKDAYS:
                continue
            candidate_dt = datetime.combine(
                candidate_date,
                dt_time(hour=SCHEDULE_HOUR, minute=SCHEDULE_MINUTE),
            )
            if candidate_dt > now:
                return candidate_dt
        raise RuntimeError("无法计算下一次定时同步时间。")

    def _next_event(self) -> tuple[str, datetime]:
        now = datetime.now()
        next_regular = self._next_regular_sync_dt(now)
        if self._pending_retry:
            retry_dt = datetime.fromisoformat(self._pending_retry["run_at"])
            if retry_dt <= now:
                return "scheduled_retry", retry_dt
            if retry_dt < next_regular:
                return "scheduled_retry", retry_dt
        return "scheduled_draw", next_regular

    def _schedule_retry(self, event_dt: datetime, latest_issue: str | None) -> None:
        retry_dt = datetime.combine(
            event_dt.date() + timedelta(days=1),
            dt_time(hour=RETRY_HOUR, minute=RETRY_MINUTE),
        )
        self._pending_retry = {
            "run_at": retry_dt.isoformat(timespec="seconds"),
            "base_issue": latest_issue or "",
        }
        print(
            "[AUTO_SYNC] 已安排补拉: "
            f"{retry_dt.isoformat(timespec='seconds')} "
            f"(基准期号 {latest_issue or '--'})"
        )

    def run(self) -> None:
        while not self._stop_event.is_set():
            event_type, event_dt = self._next_event()
            wait_seconds = max(0.0, (event_dt - datetime.now()).total_seconds())
            print(
                "[AUTO_SYNC] 下次任务: "
                f"type={event_type} at={event_dt.isoformat(timespec='seconds')}"
            )
            if self._stop_event.wait(wait_seconds):
                break

            try:
                if event_type == "scheduled_retry" and self._pending_retry:
                    current_latest = get_latest_draw()
                    current_issue = current_latest["issue"] if current_latest else ""
                    if current_issue and current_issue != self._pending_retry["base_issue"]:
                        print(
                            "[AUTO_SYNC] 跳过补拉: "
                            f"期号已从 {self._pending_retry['base_issue'] or '--'} "
                            f"更新为 {current_issue}"
                        )
                        self._pending_retry = None
                        continue

                result = sync_history(trigger_type=event_type)
                latest_issue = result.get("latest_issue") or "--"
                print(
                    "[AUTO_SYNC] "
                    f"type={event_type} status={result['status']} latest_issue={latest_issue} "
                    f"inserted={result.get('inserted_count', 0)} "
                    f"updated={result.get('updated_count', 0)}"
                )

                if event_type == "scheduled_draw":
                    if result["status"] != "synced" or not result.get("has_new_issue", False):
                        self._schedule_retry(event_dt, result.get("latest_issue"))
                    else:
                        self._pending_retry = None
                else:
                    self._pending_retry = None
            except Exception as exc:
                print(f"[AUTO_SYNC] type={event_type} failed: {exc}")
                if event_type == "scheduled_draw":
                    latest_draw = get_latest_draw()
                    latest_issue = latest_draw["issue"] if latest_draw else None
                    self._schedule_retry(event_dt, latest_issue)
                else:
                    self._pending_retry = None

    def stop(self) -> None:
        self._stop_event.set()


class SsqRequestHandler(BaseHTTPRequestHandler):
    server_version = "SSQPredictor/1.0"

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path in PAGE_ROUTES:
            self._serve_file(STATIC_DIR / "index.html", "text/html; charset=utf-8")
            return
        if parsed.path == "/api/health":
            self._serve_health()
            return
        if parsed.path == "/api/report":
            query = parse_qs(parsed.query)
            force_refresh = query.get("refresh", ["0"])[0] == "1"
            self._serve_report(force_refresh)
            return
        if parsed.path.startswith("/static/"):
            relative = parsed.path.removeprefix("/static/")
            target = (STATIC_DIR / relative).resolve()
            if STATIC_DIR.resolve() not in target.parents and target != STATIC_DIR.resolve():
                self.send_error(HTTPStatus.NOT_FOUND)
                return
            if not target.exists() or not target.is_file():
                self.send_error(HTTPStatus.NOT_FOUND)
                return
            mime_type, _ = mimetypes.guess_type(str(target))
            self._serve_file(target, mime_type or "application/octet-stream")
            return
        self.send_error(HTTPStatus.NOT_FOUND)

    def log_message(self, fmt: str, *args: object) -> None:
        sys.stdout.write(f"[HTTP] {self.address_string()} - {fmt % args}\n")

    def _send_bytes_response(
        self,
        *,
        status: HTTPStatus,
        body: bytes,
        content_type: str,
        cache_control: str | None = None,
    ) -> None:
        try:
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            if cache_control:
                self.send_header("Cache-Control", cache_control)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            sys.stdout.write(
                "[HTTP] client disconnected before response body was fully sent\n"
            )

    def _serve_file(self, path: Path, content_type: str) -> None:
        body = path.read_bytes()
        self._send_bytes_response(
            status=HTTPStatus.OK,
            body=body,
            content_type=content_type,
        )

    def _serve_health(self) -> None:
        body = json.dumps(
            {
                "status": "ok",
                "server_time": datetime.now().isoformat(timespec="seconds"),
            },
            ensure_ascii=False,
        ).encode("utf-8")
        self._send_bytes_response(
            status=HTTPStatus.OK,
            body=body,
            content_type="application/json; charset=utf-8",
            cache_control="no-store",
        )

    def _serve_report(self, force_refresh: bool) -> None:
        started = time.perf_counter()
        try:
            report = build_report(force_refresh=force_refresh)
            body = json.dumps(report, ensure_ascii=False).encode("utf-8")
            status = HTTPStatus.OK
        except Exception as exc:
            duration = time.perf_counter() - started
            sys.stdout.write(
                "[REPORT] build failed "
                f"after {duration:.2f}s: {exc.__class__.__name__}: {exc}\n"
            )
            traceback.print_exc()
            body = json.dumps(
                {
                    "error": str(exc),
                    "error_type": exc.__class__.__name__,
                },
                ensure_ascii=False,
            ).encode("utf-8")
            status = HTTPStatus.INTERNAL_SERVER_ERROR
        else:
            duration = time.perf_counter() - started
            sys.stdout.write(
                f"[REPORT] build succeeded after {duration:.2f}s\n"
            )

        self._send_bytes_response(
            status=status,
            body=body,
            content_type="application/json; charset=utf-8",
            cache_control="no-store",
        )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="双色球历史分析与预测工具")
    parser.add_argument("--host", default="127.0.0.1", help="监听地址")
    parser.add_argument("--port", type=int, default=8000, help="监听端口")
    parser.add_argument(
        "--disable-auto-sync",
        action="store_true",
        help="禁用后台自动同步",
    )
    parser.add_argument(
        "--sync-once",
        action="store_true",
        help="执行一次同步和预测后退出，可用于任务计划程序",
    )
    parser.add_argument(
        "--no-browser",
        action="store_true",
        help="启动后不自动打开浏览器",
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    auto_sync_enabled = not args.disable_auto_sync and not args.sync_once
    configure_runtime(auto_sync_enabled=auto_sync_enabled)

    if args.sync_once:
        result = sync_history(trigger_type="sync_once")
        print(
            "同步完成: "
            f"status={result['status']} "
            f"latest_issue={result.get('latest_issue') or '--'} "
            f"inserted={result.get('inserted_count', 0)} "
            f"updated={result.get('updated_count', 0)}"
        )
        if result.get("warning"):
            print(result["warning"])
        return

    server = ThreadingHTTPServer((args.host, args.port), SsqRequestHandler)
    url = f"http://{args.host}:{args.port}"
    auto_sync_worker = None

    if auto_sync_enabled:
        auto_sync_worker = AutoSyncWorker()
        auto_sync_worker.start()

    print(f"双色球分析工具已启动: {url}")
    if auto_sync_enabled:
        print(f"后台自动同步已开启，{get_schedule_description()}。")
    else:
        print("后台自动同步已关闭，需手动点击页面中的刷新按钮。")
    print("首次初始化数据库可能需要十几秒。按 Ctrl+C 退出。")

    if not args.no_browser:
        threading.Timer(0.8, lambda: webbrowser.open(url)).start()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n服务已停止。")
    finally:
        if auto_sync_worker is not None:
            auto_sync_worker.stop()
        server.server_close()


if __name__ == "__main__":
    main()
