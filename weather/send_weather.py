import os
import requests
import schedule
import time
import logging
from datetime import datetime
import pytz
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry
import sys
from pathlib import Path
from dotenv import load_dotenv

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

class Config:
    def __init__(self):
        # 如果.env文件存在则加载，不存在也不报错
        env_path = Path('.env')
        if env_path.exists():
            load_dotenv()
            logger.info("已加载 .env 文件")
        else:
            logger.info("未找到 .env 文件，将使用系统环境变量")
        
        # 必需的环境变量
        self.GAODE_API_KEY = os.environ.get('GAODE_API_KEY')
        if not self.GAODE_API_KEY:
            raise ValueError("未设置 GAODE_API_KEY 环境变量")

        # 可选的环境变量（带默认值）
        self.TIMEZONE = pytz.timezone(os.environ.get('TIMEZONE', 'Asia/Shanghai'))
        self.QQ_USER_ID = os.environ.get('QQ_USER_ID', '80116747')
        self.QQBOT_API_URL = os.environ.get('QQBOT_API_URL', 'https://bot.asbid.cn')
        self.CITY_CODE = os.environ.get('CITY_CODE', '110101')
        
        # 尝试从环境变量获取数值，如果失败则使用默认值
        try:
            self.SEND_FREQUENCY_MINUTES = int(os.environ.get('SEND_FREQUENCY_MINUTES', '15'))
        except ValueError:
            self.SEND_FREQUENCY_MINUTES = 60
            logger.warning("SEND_FREQUENCY_MINUTES 格式无效，使用默认值: 60")

        try:
            self.REQUEST_TIMEOUT = int(os.environ.get('REQUEST_TIMEOUT', '10'))
        except ValueError:
            self.REQUEST_TIMEOUT = 10
            logger.warning("REQUEST_TIMEOUT 格式无效，使用默认值: 10")

        try:
            self.MAX_RETRIES = int(os.environ.get('MAX_RETRIES', '3'))
        except ValueError:
            self.MAX_RETRIES = 3
            logger.warning("MAX_RETRIES 格式无效，使用默认值: 3")

        # 消息模板
        default_template = (
            "时间: {time}\n"
            "温度: {temperature}°C\n"
            "天气: {weather}\n"
            "湿度: {humidity}%\n"
            "风向: {winddirection}\n"
            "风力: {windpower}"
        )
        self.MESSAGE_TEMPLATE = os.environ.get('MESSAGE_TEMPLATE', default_template)

        # 日志输出当前配置
        self._log_config()

    def _log_config(self):
        """记录当前配置信息"""
        logger.info("当前配置信息:")
        logger.info(f"时区: {self.TIMEZONE}")
        logger.info(f"QQ用户ID: {self.QQ_USER_ID}")
        logger.info(f"城市代码: {self.CITY_CODE}")
        logger.info(f"发送频率: {self.SEND_FREQUENCY_MINUTES}分钟")
        logger.info(f"请求超时: {self.REQUEST_TIMEOUT}秒")
        logger.info(f"最大重试次数: {self.MAX_RETRIES}")


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
                f"🕒时间: {current_time}\n"
                f"🌡️温度: {weather_data['temperature']}°C\n"
                f"☁️ 天气: {weather_data['weather']}\n"
                f"💧湿度: {weather_data['humidity']}%\n"
                f"🌪️风向: {weather_data['winddirection']}\n"
                f"💨风力: {weather_data['windpower']}"
            )

            # 发送消息
            url = f"{self.config.QQBOT_API_URL}/send_private_msg"
            params = {
                'user_id': self.config.QQ_USER_ID,
                'message': message
            }

            response = self.http_session.get(url, params=params)
            
            if response.status_code == 200:
                logger.info(f"消息发送成功: {message}")
                logger.debug(f"完整响应: {response.text}")
            else:
                logger.error(f"消息发送失败，状态码: {response.status_code}")
                logger.error(f"响应内容: {response.text}")

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