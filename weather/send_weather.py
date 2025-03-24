import os
import requests
import schedule
import time
from datetime import datetime
import pytz  # 需要安装 pytz 库

# 设置时区
TIMEZONE = pytz.timezone('Asia/Shanghai')

# 从环境变量中读取配置
QQ_USER_ID = os.getenv('QQ_USER_ID', '80116747')  # 默认QQ号
QQBOT_API_URL = os.getenv('QQBOT_API_URL', 'https://bot.asbid.cn')  # 默认QQBOT API地址
GAODE_API_KEY = os.getenv('GAODE_API_KEY')  # 高德API Key
CITY_CODE = os.getenv('CITY_CODE', '110101')  # 默认城市编码（北京）

# 获取天气信息
def get_weather():
    url = f'https://restapi.amap.com/v3/weather/weatherInfo?city={CITY_CODE}&key={GAODE_API_KEY}'
    response = requests.get(url)
    data = response.json()
    
    if data['status'] == '1' and data['infocode'] == '10000':
        weather_info = data['lives'][0]
        temperature = weather_info['temperature']  # 当前气温
        weather = weather_info['weather']  # 天气情况
        return temperature, weather
    else:
        raise Exception(f"天气API请求失败: {data}")

# 发送消息
def send_message():
    try:
        # 获取当前时间和天气
        current_time = datetime.now(TIMEZONE).strftime('%Y-%m-%d %H:%M:%S')  # 使用指定时区
        temperature, weather = get_weather()
        
        # 构造消息内容
        message = f"当前时间: {current_time}, 当前气温: {temperature}°C, 天气情况: {weather}"
        print(f"准备发送消息: {message}")
        
        # 发送HTTP请求
        url = f'{QQBOT_API_URL}/send_private_msg?user_id={QQ_USER_ID}&message={message}'
        response = requests.get(url)
        
        if response.status_code == 200:
            print("消息发送成功！")
        else:
            print(f"消息发送失败，状态码: {response.status_code}")
    except Exception as e:
        print(f"发生错误: {e}")

# 每 15 分钟执行一次
schedule.every(45).minutes.do(send_message)

# 主循环
if __name__ == '__main__':
    print("定时任务已启动，每 15 分钟发送一次消息...")
    while True:
        schedule.run_pending()
        time.sleep(1)