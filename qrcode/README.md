# 二维码生成服务

这是一个基于Go语言开发的二维码生成服务，支持多种二维码格式和内容。

## 功能
```
http://localhost:8080/qrcode?content=https://example.com&size=300&format=svg
```
- 支持生成PNG、SVG等格式的二维码
- 支持自定义二维码大小

## 使用方法
### POST请求
POST `/qrcode`
```http
Content-Type: application/json
{
    "content": "Hello World",
    "size": 200,
    "format": "png"
}
```

### 响应:

PNG格式返回Base64编码的图片

SVG格式直接返回图像数据

### GET请求

GET `/qrcode?content=Hello World&size=200&format=png`

### 响应:

PNG格式返回Base64编码的图片
SVG格式直接返回图像数据