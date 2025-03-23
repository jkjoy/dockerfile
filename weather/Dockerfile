# 使用官方Python镜像作为基础镜像
FROM python:3.9-slim

# 设置工作目录
WORKDIR /app

# 复制当前目录下的文件到工作目录
COPY . /app

# 安装依赖
RUN pip install --no-cache-dir requests schedule

# 设置环境变量
ENV QQ_USER_ID=80116747 \
    QQBOT_API_URL=https://bot.asbid.cn/send_private_msg \
    GAODE_API_KEY=your_gaode_api_key \
    CITY_CODE=110101

# 运行脚本
CMD ["python", "send_weather.py"]