# IP地理位置查询服务

这是一个基于Go语言开发的IP地理位置查询服务，支持多种数据源进行IP归属地查询。

## 功能特性

- 支持MaxMind GeoLite2数据库查询
- 支持纯真IP库查询（针对中国IP优化）
- 多语言支持（优先显示中文）
- RESTful API接口
- Docker容器化部署

## 技术栈

- **编程语言**: Go
- **数据库**: MaxMind GeoLite2 (City + ASN)
- **IP库**: 纯真IP库 (QQWry)
- **部署**: Docker

## API使用

### 查询IP地理位置

```
GET /{ip}
```

**响应示例**:
```json
{
  "ip": "8.8.8.8",
  "country": "美国",
  "country_code": "US",
  "province": "加利福尼亚州",
  "region": "加利福尼亚州",
  "city": "山景城",
  "isp": "Google LLC",
  "latitude": 37.4056,
  "longitude": -122.0775,
  "source": "MaxMind"
}
```

**中国IP响应示例**:
```json
{
  "ip": "114.114.114.114",
  "country": "中国",
  "country_code": "CN",
  "province": "江苏",
  "region": "",
  "city": "南京",
  "isp": "电信",
  "latitude": 34.7732,
  "longitude": 113.722,
  "source": "QQWry+MaxMind"
}
```

## 环境变量

- `PORT`: 服务端口 (默认: 8080)
- `CITY_DB_PATH`: MaxMind City数据库路径 (默认: ./data/GeoLite2-City.mmdb)
- `ASN_DB_PATH`: MaxMind ASN数据库路径 (默认: ./data/GeoLite2-ASN.mmdb)
- `QQWRY_PATH`: 纯真IP库路径 (默认: ./data/qqwry.dat)

## 开发历史

### 2024年修改记录

#### 会话2: 增加省份字段并优化中国IP处理
- **主要目的**: 剔除city中的国家信息，增加省份信息字段，优化ISP信息显示
- **完成的主要任务**: 
  - 在GeoLocation结构体中增加Province字段
  - 修改中国IP处理逻辑，将纯真库的Country字段作为Province信息
  - 剔除原来将纯真库Country字段作为City信息的逻辑
  - 优化纯真库数据解析，正确拆分"中国–省份–城市"格式的数据
  - 添加ISP中文化映射功能，将英文ISP名称转换为中文
  - 优先使用纯真库的中文ISP信息
- **关键决策和解决方案**:
  - 新增Province字段用于存储省份信息
  - 解析纯真库Country字段的"中国–省份–城市"格式
  - 根据分隔符数量智能分配省份和城市信息
  - 创建ISP中文化映射表，支持常见ISP的中文显示
  - 优先使用纯真库的中文ISP信息，覆盖MaxMind的英文信息
  - 保持MaxMind的City字段作为真正的城市信息
- **使用的技术栈**: Go, MaxMind GeoLite2 API, 纯真IP库
- **修改的文件**: main.go

#### 会话1: 中文化MaxMind查询结果
- **主要目的**: 让MaxMind获取到的归属地信息显示为中文
- **完成的主要任务**: 
  - 修改queryIP函数中的MaxMind查询逻辑
  - 优先使用中文名称（zh-CN, zh），如果没有则回退到英文名称
  - 覆盖国家、地区、城市三个字段的中文化
- **关键决策和解决方案**:
  - 采用优先级策略：zh-CN > zh > en
  - 保持向后兼容，确保没有中文数据时仍能正常显示英文
- **使用的技术栈**: Go, MaxMind GeoLite2 API
- **修改的文件**: main.go

## 部署

使用Docker进行部署：

```bash
docker build -t ip-geo-service .
docker run -p 8080:8080 ip-geo-service
```

## 数据源

- MaxMind GeoLite2: 提供全球IP地理位置数据
- 纯真IP库: 提供中国IP的详细归属地信息 