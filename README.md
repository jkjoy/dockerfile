# Docker Images
Dockerfile

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

## Typecho Use Sqlite

```bash 
docker run -d -p 80:80 jkjoy/typecho
```

映射目录
```
-v /typecho:/app/data
```
## PHP8.3

## 我做了什么

- 增加拓展 `redis` `pdo_mysql` `mysqli` `gd` `intl` `opcache`
- 修改`upload_max_filesize`的值为`100MB`
- 修改`post_max_size`的值为`100MB`
- 增加`Typecho`的固定链接伪静态

## 使用
需要映射网站根目录路径 /app 到宿主机以实现持久化数据

需要映射容器端口 `80`
## 步骤

### 创建目录

```bash
mkdir data
chown -R 82:82 ./data
docker run -d -p 80:80 -v ./data:/app jkjoy/php83
```
赋予本地目录权限

```bash
mkdir data
chown -R 82:82 ./data
```

使用`docker-compose.yaml`

```yaml
services:
  php83:
    image: jkjoy/php83
    container_name: php83
    restart: always
    ports:
      - '9000:80'
    volumes:
      - ./data:/app
```

把`Typecho`源码放入`data`目录下

也可以拉取`mysql`镜像作为网站数据库,也可以使用`sqlite`.

```yaml
services:
  php83:
    image: jkjoy/php83
    container_name: typecho
    restart: always
    ports:
      - '9000:80'
    volumes:
      - ./data:/app
    depends_on:
      mysql:
        condition: service_healthy
    networks:
      - typecho_network

  mysql:
    image: mysql:8
    container_name: db
    restart: always
    environment:
      MYSQL_ROOT_PASSWORD: typecho #自行修改
      MYSQL_DATABASE: typecho #自行修改
      MYSQL_USER: typecho #自行修改
      MYSQL_PASSWORD: typecho #自行修改
    ports:
      - "3306:3306"
    volumes:
      - ./mysql:/var/lib/mysql
    networks:
      - typecho_network

  phpmyadmin:
    image: phpmyadmin
    container_name: phpmyadmin
    restart: always
    environment:
      PMA_HOST: db
    ports:
      - "8800:80"
    networks:
      - typecho_network

networks:
  typecho_network:
    driver: bridge
```

### 反向代理

nginx可能需要加入
```js
    proxy_set_header X-Forwarded-Proto $scheme; 
```
来传递协议,避免出现协议混淆

## Webhook

```bash
docker run -d \
  --name=webhook \
  -e TZ=America/New_York `#optional` \
  -v /path/to/appdata/config:/config:ro \
  -p 9000:9000 \
  --restart always \
  jkjoy/webhook:alpine \
  -verbose -hooks=hooks.yml -hotreload
```

## Pleroma on cloud

```
docker run -d \
  --name=pleroma \
  -e INSTANCE_NAME=Pleroma \
  -e DOMAIN=miantiao.me \
  -e DB_HOST=pleroma.aivencloud.com \
  -e DB_PORT=28404 \
  -e DB_NAME=pleroma \
  -e DB_USER=avnadmin \
  -e DB_PASS=AVNS_password \
  -p 4000:4000 \
  jkjoy/pleroma-on-cloud
```
### 开始部署此项目

**注意修改环境变量** 为你自己的域名和数据库地址

    ```env
    INSTANCE_NAME=Pleroma # 实例英文名称
    DOMAIN=miantiao.me # 实例域名
    DB_HOST=pleroma.aivencloud.com # 数据库主机地址
    DB_PORT=28404 # 数据库端口
    DB_NAME=pleroma # 数据库名称
    DB_USER=avnadmin # 数据库用户名
    DB_PASS=AVNS_password # 数据库密码
    ```

### 使用云平台的 Console/Shell 功能

注册你的管理员账号（Zeabur 不支持此功能建议本地/其他平台部署创建账号后再部署到 Zeabur）

    `./bin/pleroma_ctl user new fakeadmin admin@test.net --admin`

### 云平台绑定域名

管理员账号登录，进入后台配置实例信息（文件存储，Email通知等等）

   - 管理界面路径是 `/pleroma/admin/#/`
   - 修改前端为 soapbox 方法：在 Settings - Frontend - Primary 中，修改 Name 为 `soapbox` Reference 为 `static`
   - 不建议加入 Relay, 中继信息大多无用还占用数据库，你只需要关注感兴趣的用户就行
