from __future__ import annotations

import asyncio
from contextlib import suppress

from nonebot import get_driver, get_plugin_config, logger, on_command
from nonebot.permission import SUPERUSER
from nonebot.plugin import PluginMetadata

from .config import Config
from .service import MastodonNotifierService


__plugin_meta__ = PluginMetadata(
    name="mastodon-message",
    description="Poll Mastodon notifications and forward them to QQ via OneBot V11.",
    usage="/mastodon-check",
    type="application",
    supported_adapters={"~onebot.v11"},
)


plugin_config = get_plugin_config(Config)
service = MastodonNotifierService(plugin_config)
driver = get_driver()
polling_task: asyncio.Task[None] | None = None


@driver.on_startup
async def _startup() -> None:
    global polling_task

    if not plugin_config.mastodon_enabled:
        logger.info("Mastodon 通知插件已禁用")
        return

    if not plugin_config.is_ready:
        logger.warning(
            "Mastodon 通知插件未启动: 请检查 MASTODON_BASE_URL、"
            "MASTODON_ACCESS_TOKEN 和 QQ 接收目标配置"
        )
        return

    polling_task = asyncio.create_task(service.run_forever(), name="mastodon-message-poller")


@driver.on_shutdown
async def _shutdown() -> None:
    global polling_task

    if polling_task is not None:
        polling_task.cancel()
        with suppress(asyncio.CancelledError):
            await polling_task
        polling_task = None

    await service.close()


mastodon_check = on_command(
    "mastodon-check",
    aliases={"长毛象检查"},
    permission=SUPERUSER,
    priority=10,
    block=True,
)


@mastodon_check.handle()
async def _handle_mastodon_check() -> None:
    result = await service.poll_once(manual=True)
    await mastodon_check.finish(result)
