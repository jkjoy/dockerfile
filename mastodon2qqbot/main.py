import os
import json
import time
import requests
from html import unescape

# 配置
MASTODON_INSTANCE = os.getenv("MASTODON_INSTANCE", "https://xxx.xxx")
MASTODON_TOKEN = os.getenv("MASTODON_TOKEN", "xxx")
QQ_API = os.getenv("QQ_API", "https://bot.asbid.cn/send_private_msg")
QQ_ID = os.getenv("QQ_ID", "80116747")
CHECK_INTERVAL = int(os.getenv("CHECK_INTERVAL", 60)) 
STATE_FILE = "./data/state.json"

def load_last_id():
    if os.path.isfile(STATE_FILE):
        with open(STATE_FILE, "r") as f:
            state = json.load(f)
            return state.get("last_notification_id", "0")
    return "0"

def save_last_id(last_id):
    directory = os.path.dirname(STATE_FILE)
    if directory and not os.path.exists(directory):
        os.makedirs(directory, exist_ok=True)
    with open(STATE_FILE, "w") as f:
        json.dump({"last_notification_id": last_id}, f)

def strip_html(html):
    import re
    text = re.sub(r'<[^>]*>', '', html or "")
    return unescape(text)

def fetch_mastodon_notifications(since_id: str):
    url = f"{MASTODON_INSTANCE}/api/v1/notifications"
    params = [
        ("exclude_types[]", "follow"),
        ("exclude_types[]", "follow_request"),
        ("since_id", since_id)
    ]
    headers = {
        "User-Agent": "Mastodon2QQBot/1.0",
        "Authorization": f"Bearer {MASTODON_TOKEN}"
    }
    resp = requests.get(url, params=params, headers=headers)
    resp.raise_for_status()
    return resp.json()

def notification_to_message(notification):
    sender = notification["account"]
    sender_name = sender.get("display_name") or sender.get("username")
    sender_handle = "@" + sender.get("acct")
    ntype = notification["type"]

    def get_content():
        status = notification.get("status")
        return strip_html(status.get("content")) if status else "[内容不可用]"

    if ntype == "mention":
        return f"💬 {sender_name} ({sender_handle}) 提到了你:\n{get_content()}"
    elif ntype == "reply":
        return f"↩️ {sender_name} ({sender_handle}) 回复了你:\n{get_content()}"
    elif ntype == "reblog":
        return f"🔁 {sender_name} ({sender_handle}) 转发了你的嘟文:\n{get_content()}"
    elif ntype == "favourite":
        return f"⭐ {sender_name} ({sender_handle}) 喜欢了你的嘟文:\n{get_content()}"
    elif ntype == "poll":
        return f"📊 {sender_name} ({sender_handle}) 的投票已结束"
    else:
        return f"ℹ️ 新通知({ntype}) 来自 {sender_name} ({sender_handle})"

def send_to_qq(messages):
    msg = "你有 {} 条新Mastodon通知:\n\n".format(len(messages))
    msg += "\n\n".join(messages)
    msg += f"\n\n查看所有通知: {MASTODON_INSTANCE}/notifications"

    payload = {
        "user_id": QQ_ID,
        "message": msg
    }
    resp = requests.post(QQ_API, json=payload, timeout=10)
    if not resp.ok:
        print(f"发送到QQ失败: {resp.status_code} {resp.text}")
    else:
        print(f"已发送到QQ: {len(messages)} 条通知")
    resp.raise_for_status()

def check_and_push():
    last_id = load_last_id()
    try:
        notifications = fetch_mastodon_notifications(last_id)
    except Exception as e:
        print(f"获取 Mastodon 通知失败: {e}")
        return

    if notifications:
        # 按created_at倒序（新→旧），以便第0条是最新的，保存最新通知ID
        notifications.sort(key=lambda x: x["created_at"], reverse=True)
        messages = [notification_to_message(n) for n in notifications]
        send_to_qq(messages)
        latest_id = notifications[0]["id"]  # 最新通知ID
        save_last_id(latest_id)
    else:
        print("无新通知。")

def main():
    while True:
        print(f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] 开始检测...")
        check_and_push()
        time.sleep(CHECK_INTERVAL)

if __name__ == "__main__":
    main()