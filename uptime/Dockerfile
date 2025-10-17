# 使用官方轻量 Python 镜像
FROM python:3.13.9-alpine

# 避免在交互安装时出现 tzdata 交互
ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /app

# 安装系统依赖（如果需要）
#RUN apk add --no-cache build-base

# 复制文件
COPY uptime/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

COPY uptime/app.py /app/
COPY uptime/entrypoint.sh /app/
COPY uptime/templates /app/templates
RUN chmod +x /app/entrypoint.sh

EXPOSE 5000

ENTRYPOINT ["/app/entrypoint.sh"]
