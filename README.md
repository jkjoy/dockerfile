# dockerfile

### weather

Docker run

```bash
docker run -d \
  -e QQ_USER_ID=80116747 \
  -e QQBOT_API_URL=https://bot.asbid.cn \
  -e GAODE_API_KEY=你的高德APIKey \
  -e CITY_CODE=110101 \
  jkjoy/qq-weather-bot
```

环境变量名

|环境变量名	|说明	|默认值|
|---|---|---|
|QQ_USER_ID	|接收消息的QQ号	|80116747|
|QQBOT_API_URL	|QQBOT的API地址	|https://bot.asbid.cn/send_private_msg|
|GAODE_API_KEY	|高德地图的API Key	|无（必须指定）|
|CITY_CODE	|高德地图的城市编码（ADCode）	|110101（北京）|

