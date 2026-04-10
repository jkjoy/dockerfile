from pathlib import Path

import nonebot
from nonebot.adapters.onebot.v11 import Adapter as OneBotV11Adapter


BASE_DIR = Path(__file__).resolve().parent
PLUGIN_DIR = BASE_DIR / "src" / "plugins"

nonebot.init(_env_file=str(BASE_DIR / ".env"))
driver = nonebot.get_driver()
driver.register_adapter(OneBotV11Adapter)
nonebot.load_plugins(str(PLUGIN_DIR))

app = nonebot.get_asgi()


if __name__ == "__main__":
    nonebot.run()
