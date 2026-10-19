# Kayqi-Chat 首版运行与验收

本次交付保持 Next.js 静态导出和 Go 单服务结构。开发及验收使用隔离 SQLite、独立文件目录和本地模拟上游；未执行生产部署。真实上游的参数、数量、耗时与内存占用仍需在目标环境实测。

已执行的检查、浏览器场景和待验收项见 [开发验证记录](KAYQI-CHAT-VERIFICATION.md)。

## 本地运行

需要 Go 1.26.8、可用的 C 编译器及 SQLite 头文件、Node.js 与仓库指定的 pnpm。项目使用 CGO 编译 SQLite/向量扩展，不能使用 `CGO_ENABLED=0` 代替。

```powershell
pnpm install --frozen-lockfile
pnpm --filter @deeix/web build

# 使用新的绝对目录，避免连接现有数据库。
$env:DATABASE_DRIVER='sqlite'
$env:SQLITE_PATH='D:/kayqi-dev/data/kayqi.db'
$env:CACHE_DRIVER='memory'
$env:STORAGE_BACKEND='local'
$env:STORAGE_ROOT_DIR='D:/kayqi-dev/storage'
$env:FRONTEND_DIST_DIR='D:/code/ai-coding/Kayqi-Chat/apps/web/out'
$env:HTTP_PORT='8080'
$env:APP_ENV='dev'
$env:CORS_ALLOW_ORIGIN='http://127.0.0.1:8080'
$env:PUBLIC_API_BASE_URL='http://127.0.0.1:8080'
$env:PUBLIC_WEB_BASE_URL='http://127.0.0.1:8080'
# 另行设置 JWT_SECRET、DATA_ENCRYPTION_KEY 为独立随机值。
cd backend
go run ./cmd/server
```

首次管理员凭据由服务端引导生成，首次登录需完成用户名和密码初始化。所有动态规则保存在数据库中，不通过 YAML 配置积分。

非默认本机端口（例如 18080）需在前端构建前设置 `NEXT_PUBLIC_API_BASE_URL=http://127.0.0.1:18080`，服务端同时配置对应 CORS 来源。生产域名默认同源，不需写死本机地址。

### 管理员启用顺序

1. 在现有模型管理中配置可用的聊天模型与 Images 上游路由。首版积分生图接受 `openai_image_generations` / `openai_image_edits`，上游必须分别具备对应路由。
2. 打开管理后台「图片积分」。为已联调的模型保存质量、尺寸、单次数量、参考图上限及是否支持流式。只声明真实接口已验证的组合；通用模型目录不会自动视作验证结果。
3. 创建默认价格：低/中/高每张 5/7/10 积分，编辑同价。可增加特定尺寸价格；空尺寸覆盖该质量全部已验证尺寸。特定尺寸价格优先，其禁用状态也优先，不能回退到统一价格绕过暂停。
4. 保持每用户并发 1、全站并发 1。设置注册/每日赠送（初始都是 0）；按需通过用户积分页发放永久积分并填写原因。
5. 开启积分策略。后台同时把原计费设为 `billing.mode=self`，继续记录 Token/原金额估算，图片只消耗积分。改回原货币收费模式会暂停积分图片任务，防止重复扣除。

未配置价格会拒绝调用；只有明确启用的 0 积分价格才表示免费。关闭积分策略会暂停服务端图片入口。历史用户、管理员创建及导入用户不会补注册奖励。桌面本地模式沿用项目自身的能力门禁，不提供服务器积分后台。

## 交付、退款和恢复

- 上海时区当日额度优先消费，永久额度在后。每天最多领取一次，当天改赠送规则不重复发放，漏领日期不补发。
- 报价包含价格版本与规范化参数指纹。正式提交会重新核验，并通过稳定 `clientRunID`、请求摘要和数据库写锁防重复及超卖。
- 按实际保存、审核允许且成功提交到消息的唯一图片数量计费。少出图、部分文件保存失败只收交付数量；纯文字、取消、审核拒绝与失败均释放未交付部分。
- 暂存图片在文件列表、内容和参考图查询中不可见；只有附件、消息完成态和积分结算的同一个事务提交后才公开。
- 日额度退款只回到原日期；跨上海午夜的旧日退款失效，永久额度照常退回。
- 活动任务定期续租。启动及运行时每分钟对账，过期但仍在本进程执行的任务继续保留；进程中断的任务收口并回收暂存文件。不会自动重发上游付费请求。重启后旧预留通常在租约到期后的下一轮巡检释放（约 2～3 分钟）。
- 后台流水及预留页可按用户查询；`reserved` 是正在占用额度，`settled` 是已结算，`released` 是全部释放。持续未收口的记录应检查服务日志中的 `image_point_reconciliation_failed` / `image_pending_cleanup_failed`。

## 验证命令

```sh
pnpm api:check
pnpm --filter @deeix/web check
pnpm --filter @deeix/web build
cd backend
go test -p 2 ./...
bash scripts/check-go-quality.sh
go build -o ../.cache/kayqi-chat ./cmd/server
```

Windows 建议将 `TEMP`、`TMP`、`GOTMPDIR` 指向磁盘充足的工作目录，降低大规模 CGO 链接对系统盘的占用。Git 配置自动 CRLF 时，完整格式检查应在 LF checkout 中执行；本次代码可通过独立 LF 快照检查，避免批量修改无关文件。

`scripts/kayqi-mock-upstream.py` 只监听本机，可用 Python 启动：

```sh
python scripts/kayqi-mock-upstream.py --port 18081 --delay 1
```

配置测试上游 `http://127.0.0.1:18081/v1`、任意测试 Key，模型名可用 `mock-chat` / `mock-image`。图片模型声明 `image.stream=false`，质量 low/medium/high、尺寸 1024x1024、数量 1。模拟接口每次只返回一张确定性色块，可以验收部分交付；它不验证真实模型兼容性，也不能用来推算真实资源消耗。

浏览器回归：同一会话发送聊天 → 切换生图并核对预估 → 将生成图和历史上传图加入编辑（去重、移除）→ 编辑 → 切回聊天进行图片理解 → 刷新恢复 → 复用历史配置 → 按当前报价确认重生成。核对管理后台的实际交付、消费、退款与余额，并验证其他用户无权读取图片。

## SQLite 轻量部署准备

使用 `deploy/config.kayqi-sqlite.example.yaml` 和 `deploy/docker-compose.kayqi-sqlite.yml`。配置采用单数据库连接、8 MiB 页缓存、64 MiB mmap 上限、WAL 与 FULL 同步，配合单图片执行并发。文件存储和数据库都必须放在持久目录。本次未实测目标机器的峰值内存，勿据模拟接口结果放大并发。

准备生产环境时，把配置复制成 `deploy/config.yaml`，替换域名和随机密钥，配置反向代理及 HTTPS；构建和启动命令如下，**生产部署应另行执行**：

```sh
docker compose -f deploy/docker-compose.kayqi-sqlite.yml build
docker compose -f deploy/docker-compose.kayqi-sqlite.yml up -d
```

启动会自动迁移表结构。升级前完整备份；本次不新增支付或独立绘画工作台，不自动迁移原货币余额。

## 备份与恢复

首版采用停服一致性备份，避免 SQLite 与图片文件处于不同时间点：

1. 在后台关闭积分入口，等待图片任务结束，并确认预留列表没有持续未结算任务。
2. 停止应用进程/容器；正常停服会取消并收尾正在执行的任务。非正常退出后先启动服务等待对账，再正常停止。
3. 同时备份 `deploy/kayqi-data`、`deploy/kayqi-storage`、`deploy/config.yaml` 及其引用的密钥。数据库目录若仍有 `-wal`/`-shm`，一并复制。不要仅复制运行中的 `.db` 文件。
4. 保留对应 Git 提交或镜像版本、备份时间及文件校验和；备份目录限制访问。

恢复时先停服并保留当前目录副本，再将同一备份批次的数据、存储、配置及密钥恢复到空目录。使用兼容的应用版本启动，等待对账，检查管理员登录、图片读取、积分流水及预留。确认无误再重新开放积分入口。不得将恢复演练连接到生产数据库，也不得将不同批次的数据库与文件目录混用。
