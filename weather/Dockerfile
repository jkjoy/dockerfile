# 使用官方Python镜像作为基础镜像
FROM python:3.13-alpine

# 设置工作目录
WORKDIR /app

# 复制当前目录下的文件到工作目录
COPY weather/ /app

# 安装依赖
RUN apk update && \
    apk add --no-cache tzdata && \
    pip install --no-cache-dir -r /app/requirements.txt 

# 设置时区
ENV TZ=Asia/Shanghai \
    QQ_USER_ID=80116747 \
    QQBOT_API_URL=https://bot.asbid.cn \
    GAODE_API_KEY=your_gaode_api_key \
    CITY_CODE=110101 \
    SEND_FREQUENCY_MINUTES=60 \
    MAX_RETRIES="5" \
    REQUEST_TIMEOUT="15" 

# 运行脚本
CMD ["python", "send_weather.py"]