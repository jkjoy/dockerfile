# dockerfile

## QQ weather bot

仅支持gocqhttp

Docker run

```bash
docker run -d \
  -e QQ_USER_ID=80116747 \
  -e QQBOT_API_URL=https://bot.asbid.cn \
  -e GAODE_API_KEY=你的高德APIKey \
  -e CITY_CODE=110101 \
  jkjoy/qq-weather-bot
```

### 环境变量名

|环境变量名	|说明	|默认值|
|---|---|---|
|QQ_USER_ID	|接收消息的QQ号	|80116747|
|QQBOT_API_URL	|QQBOT的API地址	|https://bot.asbid.cn/send_private_msg|
|GAODE_API_KEY	|高德地图的API Key	|无（必须指定）|
|CITY_CODE	|高德地图的城市编码（ADCode）	|110101（北京）|

## Wordpress use Sqlite

```bash
    docker run -d -p 80:80 jkjoy/wordpress
```


### 环境变量
```
    environment:
      - FORCE_SSL_LOGIN=false #默认为true  强制https登录
      - FORCE_SSL_ADMIN=false #默认为true  强制https管理后台
      - HTTPS_ENABLED=false #默认为true  启用https
```
## KMS Server

```bash 
docker run -d -p 1688:1688 --name kms-server jkjoy/kms
```
