FROM python:3.13-alpine

WORKDIR /app

COPY mastodon2qqbot/main.py /app
COPY mastodon2qqbot/requirements.txt /app

RUN pip install --no-cache-dir -r /app/requirements.txt

CMD ["python", "main.py"]