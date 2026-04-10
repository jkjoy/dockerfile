from __future__ import annotations

import asyncio
import json
import re
from html import unescape
from html.parser import HTMLParser
from pathlib import Path
from typing import Any
from urllib.parse import parse_qsl, urlencode, urljoin, urlparse, urlunparse

import httpx
from nonebot import get_bots, logger
from nonebot.adapters.onebot.v11 import Bot as OneBotBot
from websockets.asyncio.client import connect
from websockets.exceptions import ConnectionClosed

from .config import Config


TYPE_LABELS = {
    "mention": "提及",
    "status": "新嘟文",
    "reblog": "转嘟",
    "follow": "关注",
    "follow_request": "关注请求",
    "favourite": "点赞",
    "poll": "投票结束",
    "update": "编辑提醒",
    "admin.sign_up": "注册提醒",
    "admin.report": "举报提醒",
}


class HTMLTextExtractor(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.parts: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag in {"br", "hr"}:
            self.parts.append("\n")
        elif tag == "li":
            self.parts.append("- ")

    def handle_endtag(self, tag: str) -> None:
        if tag in {"p", "div", "li", "blockquote"}:
            self.parts.append("\n")

    def handle_data(self, data: str) -> None:
        if data:
            self.parts.append(data)

    def get_text(self) -> str:
        text = unescape("".join(self.parts))
        text = re.sub(r"\n{3,}", "\n\n", text)
        text = "\n".join(line.strip() for line in text.splitlines() if line.strip())
        return text.strip()


def strip_html(content: str | None) -> str:
    if not content:
        return ""
    parser = HTMLTextExtractor()
    parser.feed(content)
    parser.close()
    return parser.get_text()


def shorten(text: str, limit: int) -> str:
    if len(text) <= limit:
        return text
    return f"{text[: limit - 3].rstrip()}..."


class MastodonNotifierService:
    def __init__(self, config: Config) -> None:
        self.config = config
        self.lock = asyncio.Lock()
        self.client = httpx.AsyncClient(
            base_url=self.config.mastodon_base_url or "https://invalid.local",
            headers={
                "Authorization": f"Bearer {self.config.mastodon_access_token}",
                "User-Agent": "mastodon-message/1.0",
                "Accept": "application/json",
            },
            timeout=self.config.mastodon_timeout,
            follow_redirects=True,
        )
        self.state_path = Path(self.config.mastodon_state_file)
        self.state = self._load_state()

    def _load_state(self) -> dict[str, str]:
        if not self.state_path.exists():
            return {}
        try:
            raw = json.loads(self.state_path.read_text(encoding="utf-8"))
        except Exception as exc:
            logger.warning(f"Mastodon 状态文件读取失败，将重置状态: {exc}")
            return {}
        if isinstance(raw, dict):
            return {str(key): str(value) for key, value in raw.items()}
        return {}

    def _save_state(self) -> None:
        self.state_path.parent.mkdir(parents=True, exist_ok=True)
        self.state_path.write_text(
            json.dumps(self.state, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )

    def _get_last_notification_id(self) -> str | None:
        value = self.state.get("last_notification_id")
        return value or None

    def _set_last_notification_id(self, notification_id: str) -> None:
        self.state["last_notification_id"] = str(notification_id)
        self._save_state()

    def _normalize_streaming_base_url(self, url: str) -> str:
        parsed = urlparse(url)
        scheme = parsed.scheme

        if scheme == "https":
            scheme = "wss"
        elif scheme == "http":
            scheme = "ws"
        elif scheme not in {"ws", "wss"}:
            scheme = "wss"

        netloc = parsed.netloc
        path = parsed.path.rstrip("/")

        if not netloc and parsed.path and not parsed.path.startswith("/"):
            netloc = parsed.path
            path = ""

        if not path:
            path = "/api/v1/streaming"
        elif path != "/api/v1/streaming" and not path.endswith("/api/v1/streaming"):
            path = f"{path}/api/v1/streaming"

        return urlunparse((scheme, netloc, path, "", parsed.query, ""))

    def _build_stream_subscription_url(self, base_url: str) -> str:
        parsed = urlparse(base_url)
        query = dict(parse_qsl(parsed.query, keep_blank_values=True))
        query["stream"] = "user:notification"
        return urlunparse(parsed._replace(query=urlencode(query)))

    async def _discover_streaming_base_url(self) -> str:
        if self.config.mastodon_streaming_url:
            return self._normalize_streaming_base_url(self.config.mastodon_streaming_url)

        candidates: list[str] = []

        try:
            response = await self.client.get("/api/v2/instance")
            if response.is_success:
                data = response.json()
                candidate = (
                    (((data or {}).get("configuration") or {}).get("urls") or {}).get("streaming")
                )
                if isinstance(candidate, str) and candidate:
                    candidates.append(candidate)
        except Exception as exc:
            logger.debug(f"读取 /api/v2/instance 失败: {exc}")

        try:
            response = await self.client.get("/api/v1/instance")
            if response.is_success:
                data = response.json()
                candidate = ((data or {}).get("urls") or {}).get("streaming_api")
                if isinstance(candidate, str) and candidate:
                    candidates.append(candidate)
        except Exception as exc:
            logger.debug(f"读取 /api/v1/instance 失败: {exc}")

        try:
            response = await self.client.get("/api/v1/streaming", follow_redirects=False)
            location = response.headers.get("location")
            if location:
                if "://" in location:
                    candidates.append(location)
                else:
                    candidates.append(urljoin(f"{self.config.mastodon_base_url}/", location))
        except Exception as exc:
            logger.debug(f"探测 /api/v1/streaming 失败: {exc}")

        if candidates:
            return self._normalize_streaming_base_url(candidates[0])

        return self._normalize_streaming_base_url(self.config.mastodon_base_url)

    def _notification_id(self, notification: dict[str, Any]) -> int:
        return int(str(notification["id"]))

    def _filter_unseen_notifications(
        self, notifications: list[dict[str, Any]]
    ) -> list[dict[str, Any]]:
        if not notifications:
            return []

        notifications = sorted(notifications, key=self._notification_id)
        last_id = self._get_last_notification_id()
        if not last_id:
            return notifications

        last_seen = int(last_id)
        return [
            notification
            for notification in notifications
            if self._notification_id(notification) > last_seen
        ]

    def _build_query_params(self, min_id: str | None = None, limit: int | None = None) -> list[tuple[str, str]]:
        params: list[tuple[str, str]] = [
            ("limit", str(limit or self.config.mastodon_limit)),
            ("include_filtered", json.dumps(self.config.mastodon_include_filtered)),
        ]
        if min_id:
            params.append(("min_id", min_id))
        for notification_type in self.config.mastodon_exclude_types:
            params.append(("exclude_types[]", notification_type))
        return params

    async def _fetch_notifications_page(
        self, min_id: str | None = None, limit: int | None = None
    ) -> list[dict[str, Any]]:
        response = await self.client.get(
            "/api/v1/notifications",
            params=self._build_query_params(min_id=min_id, limit=limit),
        )
        response.raise_for_status()
        data = response.json()
        if not isinstance(data, list):
            raise ValueError("Mastodon API returned unexpected payload")
        return data

    async def _fetch_latest_notification_id(self) -> str | None:
        items = await self._fetch_notifications_page(limit=1)
        if not items:
            return None
        return str(items[0]["id"])

    async def _fetch_new_notifications(self, last_id: str) -> list[dict[str, Any]]:
        cursor = last_id
        collected: dict[str, dict[str, Any]] = {}

        while True:
            page = await self._fetch_notifications_page(min_id=cursor, limit=self.config.mastodon_limit)
            if not page:
                break

            for item in page:
                collected[str(item["id"])] = item

            newest_id = max((str(item["id"]) for item in page), key=int)
            if newest_id == cursor:
                break
            cursor = newest_id

            if len(page) < self.config.mastodon_limit:
                break

        return [collected[key] for key in sorted(collected, key=int)]

    async def _dispatch_notifications(self, notifications: list[dict[str, Any]]) -> tuple[int, int, str | None]:
        unseen = self._filter_unseen_notifications(notifications)
        if not unseen:
            return 0, 0, self._get_last_notification_id()

        chunks = self._build_message_chunks(unseen)
        target_count = await self._send_chunks(chunks)
        latest_id = str(unseen[-1]["id"])
        self._set_last_notification_id(latest_id)
        return len(unseen), target_count, latest_id

    def _build_status_preview(self, status: dict[str, Any] | None) -> str:
        if not status:
            return ""

        spoiler_text = strip_html(status.get("spoiler_text"))
        content = strip_html(status.get("content"))
        media_count = len(status.get("media_attachments") or [])

        preview = content
        if spoiler_text and content:
            preview = f"CW: {spoiler_text}\n{content}"
        elif spoiler_text:
            preview = f"CW: {spoiler_text}"

        if not preview and media_count:
            preview = f"[无正文，包含 {media_count} 个附件]"
        elif not preview:
            preview = "[无正文]"

        return shorten(preview, self.config.mastodon_preview_length)

    def _format_notification(self, notification: dict[str, Any]) -> str:
        account = notification.get("account") or {}
        status = notification.get("status") or {}
        notification_type = str(notification.get("type") or "unknown")
        visibility = status.get("visibility")

        if notification_type == "mention" and visibility == "direct":
            label = "私信"
        else:
            label = TYPE_LABELS.get(notification_type, f"通知/{notification_type}")

        display_name = account.get("display_name") or account.get("username") or "unknown"
        acct = account.get("acct") or account.get("username") or "unknown"
        preview = self._build_status_preview(status)
        status_url = status.get("url") or ""

        lines = [f"{label} | {display_name} (@{acct})"]
        if preview:
            lines.append(preview)
        if status_url:
            lines.append(status_url)
        return "\n".join(lines)

    def _build_message_chunks(self, notifications: list[dict[str, Any]]) -> list[str]:
        total = len(notifications)
        header = f"Mastodon 新通知 {total} 条"
        detail_blocks = [
            f"[{index}/{total}]\n{self._format_notification(notification)}"
            for index, notification in enumerate(notifications, start=1)
        ]

        chunks: list[str] = []
        current = header

        for block in detail_blocks:
            fragment = f"\n\n{block}"
            if len(current) + len(fragment) > self.config.mastodon_message_max_length and current != header:
                chunks.append(current)
                current = f"Mastodon 新通知续报\n\n{block}"
            else:
                current += fragment

        current += f"\n\n通知页: {self.config.mastodon_base_url}/notifications"
        chunks.append(current)
        return chunks

    def _select_bot(self) -> OneBotBot | None:
        bots = [bot for bot in get_bots().values() if isinstance(bot, OneBotBot)]
        if not bots:
            return None

        if self.config.mastodon_onebot_self_id:
            for bot in bots:
                if bot.self_id == self.config.mastodon_onebot_self_id:
                    return bot
            logger.warning(
                "未找到配置的 OneBot self_id={}，将使用当前第一个在线机器人",
                self.config.mastodon_onebot_self_id,
            )

        return bots[0]

    async def _send_chunks(self, chunks: list[str]) -> int:
        bot = self._select_bot()
        if bot is None:
            raise RuntimeError("当前没有可用的 OneBot V11 机器人连接")

        target_count = 0
        for user_id in self.config.mastodon_notify_private_ids:
            for chunk in chunks:
                await bot.send_private_msg(user_id=user_id, message=chunk)
            target_count += 1

        for group_id in self.config.mastodon_notify_group_ids:
            for chunk in chunks:
                await bot.send_group_msg(group_id=group_id, message=chunk)
            target_count += 1

        return target_count

    async def _handle_stream_message(self, raw_message: str | bytes) -> str | None:
        if isinstance(raw_message, bytes):
            raw_message = raw_message.decode("utf-8")

        try:
            payload = json.loads(raw_message)
        except json.JSONDecodeError:
            logger.warning("收到无法解析的 Streaming JSON: {}", raw_message)
            return None

        if not isinstance(payload, dict):
            logger.warning("收到无法识别的 Streaming 消息: {}", raw_message)
            return None

        if "error" in payload:
            raise RuntimeError(f"Streaming API 返回错误: {payload}")

        event = payload.get("event")
        if event == "notification":
            encoded = payload.get("payload")
            if isinstance(encoded, str):
                try:
                    notification = json.loads(encoded)
                except json.JSONDecodeError:
                    logger.warning("notification payload 不是合法 JSON: {}", encoded)
                    return None
            elif isinstance(encoded, dict):
                notification = encoded
            else:
                logger.warning("notification 事件缺少 payload: {}", payload)
                return None

            async with self.lock:
                pushed, target_count, latest_id = await self._dispatch_notifications([notification])
            if pushed == 0:
                return None

            return f"已通过 Streaming 推送 {pushed} 条 Mastodon 通知，目标 {target_count} 个，最新 ID={latest_id}。"

        if event == "notifications_merged":
            result = await self.poll_once(manual=False)
            if result == "没有新的 Mastodon 通知。":
                return None
            return f"Streaming 收到 notifications_merged，{result}"

        if event not in {None, "filters_changed", "conversation", "announcement", "announcement.reaction", "announcement.delete"}:
            logger.debug("忽略 Streaming 事件: {}", event)
        return None

    async def poll_once(self, manual: bool = False) -> str:
        async with self.lock:
            if not self.config.mastodon_enabled:
                return "Mastodon 通知插件已禁用。"
            if not self.config.mastodon_base_url:
                return "未配置 MASTODON_BASE_URL。"
            if not self.config.mastodon_access_token:
                return "未配置 MASTODON_ACCESS_TOKEN。"
            if not self.config.has_targets:
                return "未配置 QQ 接收目标。请设置 MASTODON_NOTIFY_PRIVATE_IDS 或 MASTODON_NOTIFY_GROUP_IDS。"

            last_id = self._get_last_notification_id()

            if not last_id:
                if self.config.mastodon_init_skip_history:
                    latest_id = await self._fetch_latest_notification_id()
                    if latest_id:
                        self._set_last_notification_id(latest_id)
                        return f"首次同步完成，已记录最新通知 ID={latest_id}，未补发历史通知。"
                    return "首次同步完成，当前没有可记录的通知。"

                initial_batch = await self._fetch_notifications_page(limit=self.config.mastodon_limit)
                if not initial_batch:
                    return "当前没有可推送的 Mastodon 通知。"

                notifications = sorted(initial_batch, key=self._notification_id)
            else:
                notifications = await self._fetch_new_notifications(last_id)
                if not notifications:
                    return "没有新的 Mastodon 通知。"

            pushed, target_count, latest_id = await self._dispatch_notifications(notifications)
            if pushed == 0:
                return "没有新的 Mastodon 通知。"

            return (
                f"已推送 {pushed} 条 Mastodon 通知，"
                f"目标 {target_count} 个，最新 ID={latest_id}。"
            )

    async def _run_polling_forever(self) -> None:
        logger.info("Mastodon 轮询已启动，间隔 {} 秒", self.config.mastodon_check_interval)
        while True:
            try:
                result = await self.poll_once()
                if result != "没有新的 Mastodon 通知。":
                    logger.info(result)
            except Exception as exc:
                logger.exception(f"Mastodon 轮询失败: {exc}")
            await asyncio.sleep(self.config.mastodon_check_interval)

    async def _run_streaming_forever(self) -> None:
        logger.info("Mastodon Streaming 已启动，重连间隔 {} 秒", self.config.mastodon_stream_reconnect_delay)

        try:
            initial_result = await self.poll_once()
            if initial_result != "没有新的 Mastodon 通知。":
                logger.info(initial_result)
        except Exception as exc:
            logger.exception(f"Mastodon Streaming 启动前同步失败: {exc}")

        while True:
            try:
                stream_base_url = await self._discover_streaming_base_url()
                stream_url = self._build_stream_subscription_url(stream_base_url)
                logger.info("正在连接 Mastodon Streaming API: {}", stream_url)

                async with connect(
                    stream_url,
                    additional_headers={
                        "Authorization": f"Bearer {self.config.mastodon_access_token}",
                        "Accept": "application/json",
                    },
                    user_agent_header="mastodon-message/1.0",
                    open_timeout=self.config.mastodon_timeout,
                    ping_interval=self.config.mastodon_stream_ping_interval,
                    ping_timeout=self.config.mastodon_stream_ping_timeout,
                    close_timeout=self.config.mastodon_timeout,
                    max_size=2**20,
                ) as websocket:
                    logger.info("Mastodon Streaming 连接成功")

                    catch_up = await self.poll_once()
                    if catch_up != "没有新的 Mastodon 通知。":
                        logger.info("Streaming 建连后补偿同步: {}", catch_up)

                    async for message in websocket:
                        result = await self._handle_stream_message(message)
                        if result:
                            logger.info(result)

            except asyncio.CancelledError:
                raise
            except ConnectionClosed as exc:
                logger.warning(
                    "Mastodon Streaming 连接断开 code={} reason={}",
                    exc.code,
                    exc.reason,
                )
            except Exception as exc:
                logger.exception(f"Mastodon Streaming 失败: {exc}")

            await asyncio.sleep(self.config.mastodon_stream_reconnect_delay)

    async def run_forever(self) -> None:
        try:
            if self.config.mastodon_transport == "polling":
                await self._run_polling_forever()
            else:
                await self._run_streaming_forever()
        except asyncio.CancelledError:
            logger.info("Mastodon 通知任务已停止")
            raise

    async def close(self) -> None:
        await self.client.aclose()
