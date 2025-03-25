import os
import requests
import schedule
import time
import logging
from datetime import datetime
import pytz
from urllib.parse import quote
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry
import sys

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.StreamHandler(sys.stdout),
        logging.FileHandler('weather_notify.log')
    ]
)
logger = logging.getLogger(__name__)

# 环境变量配置类
class Config:
    def __init__(self):
        # 必需的环境变量
        self.GAODE_API_KEY = os.getenv('GAODE_API_KEY')
        if not self.GAODE_API_KEY:
            raise ValueError("请设置 GAODE_API_KEY 环境变量")

        # 可选的环境变量（带默认值）
        self.TIMEZONE = pytz.timezone(os.getenv('TIMEZONE', 'Asia/Shanghai'))
        self.QQ_USER_ID = os.getenv('QQ_USER_ID', '80116747')
        self.QQBOT_API_URL = os.getenv('QQBOT_API_URL', 'https://bot.asbid.cn')
        self.CITY_CODE = os.getenv('CITY_CODE', '110101')
        self.SEND_FREQUENCY_MINUTES = int(os.getenv('SEND_FREQUENCY_MINUTES', '60'))
        self.REQUEST_TIMEOUT = int(os.getenv('REQUEST_TIMEOUT', '10'))
        self.MAX_RETRIES = int(os.getenv('MAX_RETRIES', '3'))

# HTTP 会话类
class HTTPSession:
    def __init__(self, config):
        self.config = config
        self.session = requests.Session()
        
        # 配置重试策略
        retry_strategy = Retry(
            total=config.MAX_RETRIES,
            backoff_factor=1,
            status_forcelist=[500, 502, 503, 504]
        )
        
        adapter = HTTPAdapter(max_retries=retry_strategy)
        self.session.mount("http://", adapter)
        self.session.mount("https://", adapter)

    def get(self, url, params=None):
        try:
            response = self.session.get(
                url,
                params=params,
                timeout=self.config.REQUEST_TIMEOUT
            )
            response.raise_for_status()
            return response
        except requests.exceptions.RequestException as e:
            logger.error(f"HTTP请求失败: {str(e)}")
            raise

class WeatherNotifier:
    def __init__(self):
        self.config = Config()
        self.http_session = HTTPSession(self.config)

    def get_weather(self):
        """获取天气信息"""
        url = 'https://restapi.amap.com/v3/weather/weatherInfo'
        params = {
            'city': self.config.CITY_CODE,
            'key': self.config.GAODE_API_KEY
        }

        try:
            response = self.http_session.get(url, params=params)
            data = response.json()

            if data['status'] == '1' and data['infocode'] == '10000':
                weather_info = data['lives'][0]
                return {
                    'temperature': weather_info['temperature'],
                    'weather': weather_info['weather'],
                    'humidity': weather_info.get('humidity', 'N/A'),
                    'winddirection': weather_info.get('winddirection', 'N/A'),
                    'windpower': weather_info.get('windpower', 'N/A')
                }
            else:
                raise Exception(f"天气API返回错误: {data}")
        except Exception as e:
            logger.error(f"获取天气信息失败: {str(e)}")
            raise

    def send_message(self):
        """发送消息"""
        try:
            # 获取当前时间和天气
            current_time = datetime.now(self.config.TIMEZONE).strftime('%Y-%m-%d %H:%M:%S')
            weather_data = self.get_weather()

            # 构造消息内容
            message = (
                f"🕒 时间: {current_time}\n"
                f"🌡️ 温度: {weather_data['temperature']}°C\n"
                f"☁️ 天气: {weather_data['weather']}\n"
                f"💧 湿度: {weather_data['humidity']}%\n"
                f"🌪️ 风向: {weather_data['winddirection']}\n"
                f"💨 风力: {weather_data['windpower']}"
            )

            # URL编码消息内容
            encoded_message = quote(message)
            
            # 构造请求URL
            url = f"{self.config.QQBOT_API_URL}/send_private_msg"
            params = {
                'user_id': self.config.QQ_USER_ID,
                'message': encoded_message
            }

            # 发送消息
            response = self.http_session.get(url, params=params)
            
            if response.status_code == 200:
                logger.info("消息发送成功")
            else:
                logger.error(f"消息发送失败，状态码: {response.status_code}")

        except Exception as e:
            logger.error(f"发送消息时发生错误: {str(e)}")

def main():
    try:
        notifier = WeatherNotifier()
        
        # 设置定时任务
        schedule.every(notifier.config.SEND_FREQUENCY_MINUTES).minutes.do(
            notifier.send_message
        )
        
        logger.info(
            f"定时任务已启动，每 {notifier.config.SEND_FREQUENCY_MINUTES} 分钟发送一次消息..."
        )
        
        # 立即发送一次消息
        notifier.send_message()
        
        # 主循环
        while True:
            schedule.run_pending()
            time.sleep(1)
            
    except KeyboardInterrupt:
        logger.info("程序被用户中断")
        sys.exit(0)
    except Exception as e:
        logger.error(f"程序发生错误: {str(e)}")
        sys.exit(1)

if __name__ == '__main__':
    main()