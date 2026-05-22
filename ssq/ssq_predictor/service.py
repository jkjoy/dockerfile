from __future__ import annotations

import re
import threading
import time
from datetime import datetime, time as dt_time, timedelta
from pathlib import Path
from typing import Any

from . import data_source
from .analysis import MIN_BACKTEST_DRAWS, build_prediction_artifacts, build_report_from_draws
from .db import (
    DEFAULT_DB_PATH,
    get_all_draws,
    get_draw_count,
    get_latest_draw,
    get_prediction_evaluation_index,
    get_prediction_performance_summary,
    get_latest_prediction_snapshot,
    get_latest_sync_run,
    get_prediction_snapshot,
    get_prediction_snapshot_index,
    get_snapshots_ready_for_evaluation,
    initialize_database,
    record_sync_run,
    save_prediction_evaluation,
    save_prediction_evaluations_bulk,
    save_prediction_snapshot,
    save_prediction_snapshots_bulk,
    upsert_draws,
)

MODEL_VERSION = "frequency-omission-zscore-v2"
SCHEDULE_WEEKDAYS = (1, 3, 6)
SCHEDULE_WEEKDAY_LABELS = ("二", "四", "日")
SCHEDULE_HOUR = 21
SCHEDULE_MINUTE = 50
RETRY_HOUR = 0
RETRY_MINUTE = 30
SCHEDULE_DESCRIPTION = "每周二、四、日 21:50 自动同步，失败则次日 00:30 重试一次"
SYNC_LOCK = threading.Lock()
BACKFILL_LOCK = threading.Lock()
_RUNTIME_OPTIONS: dict[str, Any] = {
    "auto_sync_enabled": False,
    "schedule_description": SCHEDULE_DESCRIPTION,
}
PRIZE_LEVEL_ORDER = ("一等奖", "二等奖", "三等奖", "四等奖", "五等奖", "六等奖")
BACKFILL_BATCH_LIMIT = 80
BACKFILL_TIME_BUDGET_SECONDS = 8.0


def _now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def configure_runtime(
    *,
    auto_sync_enabled: bool,
) -> None:
    _RUNTIME_OPTIONS["auto_sync_enabled"] = auto_sync_enabled
    _RUNTIME_OPTIONS["schedule_description"] = SCHEDULE_DESCRIPTION


def get_runtime_options() -> dict[str, Any]:
    return dict(_RUNTIME_OPTIONS)


def get_schedule_description() -> str:
    return SCHEDULE_DESCRIPTION


def get_next_regular_sync_at(now: datetime | None = None) -> str:
    current = now or datetime.now()
    for offset in range(0, 8):
        candidate_date = current.date() + timedelta(days=offset)
        if candidate_date.weekday() not in SCHEDULE_WEEKDAYS:
            continue
        candidate_dt = datetime.combine(
            candidate_date,
            dt_time(hour=SCHEDULE_HOUR, minute=SCHEDULE_MINUTE),
        )
        if candidate_dt > current:
            return candidate_dt.isoformat(timespec="seconds")
    raise RuntimeError("无法计算下一次双色球定时同步时间。")


def get_retry_sync_time_label() -> str:
    return f"{RETRY_HOUR:02d}:{RETRY_MINUTE:02d}"


def _infer_next_issue(issue: str) -> str | None:
    if not re.fullmatch(r"\d{7}", issue):
        return None
    year = int(issue[:4])
    seq = int(issue[4:]) + 1
    if seq <= 999:
        return f"{year}{seq:03d}"
    return None


def _build_prediction_snapshot_from_draws(draws: list[dict[str, Any]]) -> dict[str, Any]:
    artifacts = build_prediction_artifacts(draws)
    latest_issue = artifacts["latest_issue_analysis"]
    prediction = artifacts["prediction"]
    return {
        "base_issue": latest_issue["issue"],
        "base_date": latest_issue["date"],
        "target_issue": _infer_next_issue(latest_issue["issue"]),
        "target_label": "下一期开奖",
        "generated_at": _now_iso(),
        "model_version": MODEL_VERSION,
        "draw_count": len(draws),
        "top_red_numbers": [item["number"] for item in prediction["top_red_numbers"]],
        "top_blue_numbers": [item["number"] for item in prediction["top_blue_numbers"]],
        "tickets": prediction["tickets"],
        "summary": {
            "latest_issue": latest_issue["issue"],
            "latest_date": latest_issue["date"],
            "strict_combo_probability_percent": latest_issue[
                "strict_combo_probability_percent"
            ],
            "historical_score_percentile": latest_issue[
                "historical_score_percentile"
            ],
        },
    }


def _determine_prize_level(red_matches: int, blue_match: int) -> str | None:
    if red_matches == 6 and blue_match == 1:
        return "一等奖"
    if red_matches == 6 and blue_match == 0:
        return "二等奖"
    if red_matches == 5 and blue_match == 1:
        return "三等奖"
    if red_matches == 5 or (red_matches == 4 and blue_match == 1):
        return "四等奖"
    if red_matches == 4 or (red_matches == 3 and blue_match == 1):
        return "五等奖"
    if blue_match == 1 and red_matches in (0, 1, 2):
        return "六等奖"
    return None


def _evaluate_prediction_snapshot(
    snapshot: dict[str, Any],
    target_draw: dict[str, Any],
) -> dict[str, Any]:
    actual_red_numbers = target_draw["red_numbers"]
    actual_blue_number = target_draw["blue_number"]
    prize_breakdown = {prize_name: 0 for prize_name in PRIZE_LEVEL_ORDER}
    ticket_results = []

    for ticket in snapshot["tickets"]:
        red_matches = len(set(ticket["red_numbers"]) & set(actual_red_numbers))
        blue_match = 1 if ticket["blue_number"] == actual_blue_number else 0
        prize_level = _determine_prize_level(red_matches, blue_match)
        if prize_level:
            prize_breakdown[prize_level] += 1
        ticket_results.append(
            {
                "rank": ticket["rank"],
                "red_numbers": ticket["red_numbers"],
                "blue_number": ticket["blue_number"],
                "score": ticket["score"],
                "summary": ticket["summary"],
                "red_match_count": red_matches,
                "blue_match": bool(blue_match),
                "prize_level": prize_level,
                "is_winner": prize_level is not None,
            }
        )

    winning_ticket_count = sum(1 for result in ticket_results if result["is_winner"])
    highest_prize_level = next(
        (prize_name for prize_name in PRIZE_LEVEL_ORDER if prize_breakdown[prize_name] > 0),
        None,
    )
    return {
        "snapshot_id": snapshot["id"],
        "base_issue": snapshot["base_issue"],
        "target_issue": target_draw["issue"],
        "target_date": target_draw["date"],
        "actual_red_numbers": ",".join(actual_red_numbers),
        "actual_blue_number": actual_blue_number,
        "evaluated_at": _now_iso(),
        "ticket_count": len(ticket_results),
        "winning_ticket_count": winning_ticket_count,
        "winning_ticket_rate": round(
            winning_ticket_count / len(ticket_results) * 100, 2
        )
        if ticket_results
        else 0.0,
        "highest_prize_level": highest_prize_level,
        "prize_breakdown": prize_breakdown,
        "ticket_results": ticket_results,
        "model_version": snapshot["model_version"],
    }


def _rebuild_prediction_if_needed(
    draws: list[dict[str, Any]],
    *,
    force: bool = False,
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, Any] | None:
    if not draws:
        return None
    latest_issue = draws[-1]["issue"]
    existing = get_prediction_snapshot(latest_issue, db_path=db_path)
    if existing and existing["model_version"] == MODEL_VERSION and not force:
        return existing

    snapshot = _build_prediction_snapshot_from_draws(draws)
    save_prediction_snapshot(snapshot, db_path=db_path)
    return get_prediction_snapshot(latest_issue, db_path=db_path)


def _evaluate_pending_predictions(db_path: Path = DEFAULT_DB_PATH) -> dict[str, int]:
    ready_snapshots = get_snapshots_ready_for_evaluation(db_path=db_path)
    evaluated_count = 0
    winning_issue_count = 0
    for snapshot in ready_snapshots:
        target_draw = {
            "issue": snapshot["target_issue"],
            "date": snapshot["target_date"],
            "red_numbers": snapshot["target_red_numbers"],
            "blue_number": snapshot["target_blue_number"],
        }
        evaluation = _evaluate_prediction_snapshot(snapshot, target_draw)
        save_prediction_evaluation(evaluation, db_path=db_path)
        evaluated_count += 1
        if evaluation["winning_ticket_count"] > 0:
            winning_issue_count += 1
    return {
        "evaluated_count": evaluated_count,
        "winning_issue_count": winning_issue_count,
    }


def _expected_prediction_counts(draw_count: int) -> tuple[int, int]:
    if draw_count < MIN_BACKTEST_DRAWS:
        return (1 if draw_count > 0 else 0, 0)
    snapshot_total = draw_count - MIN_BACKTEST_DRAWS + 1
    evaluation_total = max(0, snapshot_total - 1)
    return snapshot_total, evaluation_total


def _backfill_prediction_history(
    draws: list[dict[str, Any]],
    *,
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, Any]:
    if not draws:
        return {
            "expected_snapshot_total": 0,
            "expected_evaluation_total": 0,
            "snapshot_created": 0,
            "snapshot_updated": 0,
            "evaluation_created": 0,
            "evaluation_updated": 0,
            "completed": True,
        }

    expected_snapshot_total, expected_evaluation_total = _expected_prediction_counts(
        len(draws)
    )
    if len(draws) < MIN_BACKTEST_DRAWS:
        return {
            "expected_snapshot_total": expected_snapshot_total,
            "expected_evaluation_total": expected_evaluation_total,
            "snapshot_created": 0,
            "snapshot_updated": 0,
            "evaluation_created": 0,
            "evaluation_updated": 0,
            "completed": True,
        }

    with BACKFILL_LOCK:
        snapshot_index = get_prediction_snapshot_index(db_path=db_path)
        evaluation_index = get_prediction_evaluation_index(db_path=db_path)
        matching_snapshot_total = sum(
            1 for item in snapshot_index.values() if item["model_version"] == MODEL_VERSION
        )
        matching_evaluation_total = sum(
            1 for item in evaluation_index.values() if item["model_version"] == MODEL_VERSION
        )
        if (
            matching_snapshot_total >= expected_snapshot_total
            and matching_evaluation_total >= expected_evaluation_total
        ):
            return {
                "expected_snapshot_total": expected_snapshot_total,
                "expected_evaluation_total": expected_evaluation_total,
                "snapshot_created": 0,
                "snapshot_updated": 0,
                "evaluation_created": 0,
                "evaluation_updated": 0,
                "completed": True,
            }

        snapshot_created = 0
        snapshot_updated = 0
        evaluation_created = 0
        evaluation_updated = 0
        snapshot_payloads: list[dict[str, Any]] = []
        evaluation_inputs: list[dict[str, Any]] = []
        start_index = MIN_BACKTEST_DRAWS - 1
        batch_started_at = time.perf_counter()
        completed = True
        processed_missing_count = 0

        for base_index in range(len(draws) - 2, start_index - 1, -1):
            if processed_missing_count >= BACKFILL_BATCH_LIMIT:
                completed = False
                break
            if time.perf_counter() - batch_started_at >= BACKFILL_TIME_BUDGET_SECONDS:
                completed = False
                break

            base_issue = draws[base_index]["issue"]
            snapshot_meta = snapshot_index.get(base_issue)
            needs_snapshot = (
                snapshot_meta is None or snapshot_meta["model_version"] != MODEL_VERSION
            )

            evaluation_meta = evaluation_index.get(base_issue)
            needs_evaluation = (
                evaluation_meta is None or evaluation_meta["model_version"] != MODEL_VERSION
            )
            if not needs_snapshot and not needs_evaluation:
                continue

            processed_missing_count += 1
            base_draws = draws[: base_index + 1]
            snapshot_payload = None
            if needs_snapshot:
                snapshot_payload = _build_prediction_snapshot_from_draws(base_draws)
                snapshot_payloads.append(snapshot_payload)
                if snapshot_meta is None:
                    snapshot_created += 1
                else:
                    snapshot_updated += 1

            if not needs_evaluation:
                continue

            evaluation_inputs.append(
                {
                    "base_issue": base_issue,
                    "target_draw": draws[base_index + 1],
                    "snapshot_payload": snapshot_payload,
                }
            )
            if evaluation_meta is None:
                evaluation_created += 1
            else:
                evaluation_updated += 1

        if snapshot_payloads:
            save_prediction_snapshots_bulk(snapshot_payloads, db_path=db_path)
            snapshot_index = get_prediction_snapshot_index(db_path=db_path)

        evaluation_payloads = []
        for item in evaluation_inputs:
            base_issue = item["base_issue"]
            snapshot_payload = item["snapshot_payload"]
            snapshot_meta = snapshot_index.get(base_issue)
            if snapshot_meta is None:
                continue
            if snapshot_payload is None:
                snapshot = get_prediction_snapshot(base_issue, db_path=db_path)
            else:
                snapshot = {
                    **snapshot_payload,
                    "id": snapshot_meta["id"],
                }
            evaluation_payloads.append(
                _evaluate_prediction_snapshot(snapshot, item["target_draw"])
            )

        if evaluation_payloads:
            save_prediction_evaluations_bulk(evaluation_payloads, db_path=db_path)

        return {
            "expected_snapshot_total": expected_snapshot_total,
            "expected_evaluation_total": expected_evaluation_total,
            "snapshot_created": snapshot_created,
            "snapshot_updated": snapshot_updated,
            "evaluation_created": evaluation_created,
            "evaluation_updated": evaluation_updated,
            "completed": completed,
            "batch_limit": BACKFILL_BATCH_LIMIT,
            "processed_missing_count": processed_missing_count,
        }


def _fetch_for_sync(
    *,
    force_full_refresh: bool,
    current_latest_issue: str | None,
) -> tuple[str, dict[str, Any]]:
    if force_full_refresh or not current_latest_issue:
        return "full", data_source.fetch_history_from_official()
    return "incremental", data_source.fetch_history_from_official(
        stop_issue=current_latest_issue
    )


def sync_history(
    *,
    force_full_refresh: bool = False,
    trigger_type: str = "manual",
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, Any]:
    with SYNC_LOCK:
        initialize_database(db_path)
        started_at = _now_iso()
        existing_count = get_draw_count(db_path)
        latest_before = get_latest_draw(db_path)
        latest_before_issue = latest_before["issue"] if latest_before else None

        try:
            sync_mode, payload = _fetch_for_sync(
                force_full_refresh=force_full_refresh,
                current_latest_issue=latest_before_issue,
            )
            source_name = payload.get("source_name", "official")
            source_warning = payload.get("source_warning", "")
            upsert_result = upsert_draws(payload["draws"], db_path=db_path)
            latest_after = get_latest_draw(db_path)
            finished_at = _now_iso()
            record_sync_run(
                started_at=started_at,
                finished_at=finished_at,
                trigger_type=trigger_type,
                sync_mode=sync_mode,
                status="success",
                fetched_count=len(payload["draws"]),
                inserted_count=upsert_result["inserted_count"],
                updated_count=upsert_result["updated_count"],
                latest_issue=latest_after["issue"] if latest_after else None,
                message=(
                    (
                        "同步完成。"
                        if source_name == "official"
                        else f"同步完成，已使用备用数据源 {source_name}。"
                    )
                    if upsert_result["inserted_count"] or upsert_result["updated_count"]
                    else (
                        "未发现新开奖数据。"
                        if source_name == "official"
                        else f"未发现新开奖数据，本次使用备用数据源 {source_name}。"
                    )
                ),
                db_path=db_path,
            )

            draws = get_all_draws(db_path=db_path)
            snapshot = _rebuild_prediction_if_needed(
                draws,
                force=(
                    force_full_refresh
                    or upsert_result["inserted_count"] > 0
                    or upsert_result["updated_count"] > 0
                ),
                db_path=db_path,
            )
            evaluation_meta = _evaluate_pending_predictions(db_path=db_path)
            return {
                "status": "synced",
                "warning": source_warning,
                "sync_mode": sync_mode,
                "fetched_count": len(payload["draws"]),
                "inserted_count": upsert_result["inserted_count"],
                "updated_count": upsert_result["updated_count"],
                "has_new_issue": upsert_result["inserted_count"] > 0,
                "evaluated_prediction_count": evaluation_meta["evaluated_count"],
                "latest_issue": latest_after["issue"] if latest_after else None,
                "latest_prediction": snapshot,
                "source_name": source_name,
            }
        except Exception as exc:
            finished_at = _now_iso()
            sync_mode = "full" if force_full_refresh or not latest_before_issue else "incremental"
            record_sync_run(
                started_at=started_at,
                finished_at=finished_at,
                trigger_type=trigger_type,
                sync_mode=sync_mode,
                status="failed",
                fetched_count=0,
                inserted_count=0,
                updated_count=0,
                latest_issue=latest_before_issue,
                message=str(exc),
                db_path=db_path,
            )
            if existing_count > 0:
                return {
                    "status": "stale_db",
                    "warning": (
                        "官方接口请求失败，当前展示的是本地数据库中的历史数据。"
                        f"失败原因: {exc}"
                    ),
                    "sync_mode": sync_mode,
                    "fetched_count": 0,
                    "inserted_count": 0,
                    "updated_count": 0,
                    "has_new_issue": False,
                    "evaluated_prediction_count": 0,
                    "latest_issue": latest_before_issue,
                    "latest_prediction": get_latest_prediction_snapshot(db_path=db_path),
                }
            raise


def ensure_data_available(db_path: Path = DEFAULT_DB_PATH) -> dict[str, Any]:
    initialize_database(db_path)
    if get_draw_count(db_path) == 0:
        return sync_history(
            force_full_refresh=True,
            trigger_type="bootstrap",
            db_path=db_path,
        )
    latest_draw = get_latest_draw(db_path)
    if latest_draw:
        draws = get_all_draws(db_path=db_path)
        _rebuild_prediction_if_needed(draws, db_path=db_path)
        _evaluate_pending_predictions(db_path=db_path)
    return {
        "status": "database",
        "warning": "",
        "sync_mode": "database",
        "fetched_count": 0,
        "inserted_count": 0,
        "updated_count": 0,
        "has_new_issue": False,
        "evaluated_prediction_count": 0,
        "latest_issue": latest_draw["issue"] if latest_draw else None,
        "latest_prediction": get_latest_prediction_snapshot(db_path=db_path),
    }


def build_report_data(
    *,
    force_refresh: bool = False,
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, Any]:
    sync_meta = (
        sync_history(
            force_full_refresh=False,
            trigger_type="manual_refresh",
            db_path=db_path,
        )
        if force_refresh
        else ensure_data_available(db_path=db_path)
    )

    draws = get_all_draws(db_path=db_path)
    if not draws:
        raise RuntimeError("数据库中暂无双色球数据。")

    latest_prediction = _rebuild_prediction_if_needed(draws, db_path=db_path)
    pending_evaluation_meta = _evaluate_pending_predictions(db_path=db_path)
    history_backfill = _backfill_prediction_history(draws, db_path=db_path)
    latest_sync = get_latest_sync_run(db_path=db_path)
    latest_prediction = get_latest_prediction_snapshot(db_path=db_path) or latest_prediction
    prediction_performance = get_prediction_performance_summary(db_path=db_path)

    report = build_report_from_draws(
        draws,
        generated_at=_now_iso(),
        data_status=sync_meta["status"],
        data_warning=sync_meta.get("warning", ""),
        sources={
            "official_page_url": data_source.OFFICIAL_PAGE_URL,
            "official_api_url": data_source.OFFICIAL_API_URL,
            "fallback_page_url": data_source.FALLBACK_SOURCE_PAGE_URL,
            "fallback_api_url": data_source.FALLBACK_API_URL,
        },
        database={
            "path": str(db_path.resolve()),
            "draw_count": len(draws),
        },
        automation={
            "auto_sync_enabled": _RUNTIME_OPTIONS["auto_sync_enabled"],
            "schedule_description": _RUNTIME_OPTIONS["schedule_description"],
            "next_regular_sync_at": get_next_regular_sync_at(),
            "retry_time_label": get_retry_sync_time_label(),
            "last_sync": latest_sync,
            "latest_prediction": latest_prediction,
            "prediction_performance": prediction_performance,
            "history_backfill": history_backfill,
            "pending_evaluation_meta": pending_evaluation_meta,
        },
        latest_prediction_snapshot=latest_prediction,
    )
    return report
