FROM python:3.13-alpine

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    TZ=Asia/Shanghai

WORKDIR /app

RUN apk add --no-cache tzdata ca-certificates \
    && ln -snf "/usr/share/zoneinfo/${TZ}" /etc/localtime \
    && echo "${TZ}" > /etc/timezone \
    && mkdir -p /app/data

COPY requirements.txt .

RUN pip install --no-cache-dir --disable-pip-version-check -r requirements.txt

COPY app.py .
COPY ssq_predictor ./ssq_predictor
COPY static ./static

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8000/api/health', timeout=5).read()"

CMD ["python", "app.py", "--host", "0.0.0.0", "--port", "8000", "--no-browser"]
