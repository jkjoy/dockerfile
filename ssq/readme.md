双色球历史分析与预测工具

运行方式：

```bash
python app.py
```

默认会启动本地服务并自动打开浏览器：`http://127.0.0.1:8000`

Docker 部署：

```bash
docker compose up -d --build
```

如果本机 Docker Compose 仍提示 `project name must not be empty`，可显式指定项目名：

```bash
docker compose -p ssq-predictor up -d --build
```

容器启动后访问：

```bash
http://127.0.0.1:8000
```

停止容器：

```bash
docker compose down
```

只执行一次同步并退出：

```bash
python app.py --sync-once
```

关闭后台自动同步：

```bash
python app.py --disable-auto-sync
```

功能：

- 拉取中国福利彩票官网双色球历史开奖数据并写入本地 SQLite 数据库
- 后台按固定时间自动同步：每周二、四、日 `21:50`
- 若当次同步失败，或 `21:50` 尚未拉到新一期数据，则次日 `00:30` 自动补拉一次
- 一旦发现新期开奖就自动入库
- 每次同步后自动生成下一期预测快照并持久化保存
- 新期开奖入库后，自动把上一期预测和真实开奖号码对比，判断是否中奖、属于几等奖
- 自动累计历史回测结果，统计期级中奖率、票级中奖率和各奖级命中次数
- 查询往期开奖，支持按期号或号码搜索
- 检查历史上是否出现过完全相同的号码组合
- 统计红球 `01-33` 和蓝球 `01-16` 的出现频率、近期热度和遗漏值
- 计算最新一期号码的严格数学概率和历史频率评分
- 基于历史频率、近30/60期趋势和遗漏值生成下一期候选号码

本地数据文件：

- SQLite 数据库：`data/ssq.db`
- 兼容性缓存文件：`data/ssq_history.json`

Docker 持久化：

- `docker-compose.yaml` 默认使用 Docker named volume `ssq_predictor_data`
- 这样可以避免宿主机目录权限、OneDrive 同步目录、Windows 文件共享等导致的 SQLite 只读问题
- 重建容器后，数据库和预测回测结果会继续保留
- 当前 Docker 运行时基于 `python:3.13-alpine`，镜像体积比 `slim` 方案更小

数据库中的核心表：

- `draws`：历史开奖数据
- `sync_runs`：每次同步记录
- `prediction_snapshots`：每一期生成的预测快照
- `prediction_evaluations`：开奖后对预测结果的验票和奖级统计

数据来源：

- 开奖页：<https://www.cwl.gov.cn/ygkj/wqkjgg/ssq/>
- 官方接口：<https://www.cwl.gov.cn/cwl_admin/front/cwlkj/search/kjxx/findDrawNotice>

说明：

- 双色球开奖是随机事件，历史统计不能保证下一期结果。
- 页面中的“预测指数”是经验排序，不是确定性概率。
- 首次运行会补历史预测快照和历史回测，数据量较大时可能需要接近 1 分钟。
- Docker 镜像内默认时区为 `Asia/Shanghai`，以保证自动同步时间和页面展示时间一致。
- 如果后续加入依赖本地编译或二进制扩展的 Python 包，Alpine 方案可能不如 `slim` 兼容，这时再切回 Debian 系镜像更稳。
- 如果之前用的是宿主机 `./data` 绑定挂载，出现 `attempt to write a readonly database` 时，优先改用当前默认的 named volume 方案。
- 如果要实现完全无人值守，可以把 `python app.py --sync-once` 配到 Windows 任务计划程序里定时执行。

