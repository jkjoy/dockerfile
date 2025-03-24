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