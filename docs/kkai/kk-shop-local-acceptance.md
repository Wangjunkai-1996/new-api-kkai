# KK 商城本地真实 HTTP 验收

在应用仓库根目录执行。需要 Go、Python 3、Docker Desktop 的 `desktop-linux` context，以及已缓存的 `postgres:18.4-bookworm`。本流程只创建本机临时 PostgreSQL 和虚构商品；不访问生产、不使用生产 DSN、不构建生产镜像。后端固定使用 `http://localhost:3000`，该端口必须空闲。

实际应用路由、Host 解析、密码登录、会话刷新、操作安全凭证、数据库、leader lease、maintenance 和 outbox 均参与验收。只有外部 Epay 网关由本机 TLS 服务模拟。应用使用本机 HTTP Cookie 模式，因此本流程不覆盖生产 HTTPS/Secure Cookie/反向代理，也不覆盖 2FA、Passkey 或真实商户资金。

1. 创建受限目录和独立 PostgreSQL。保留这个终端中的 `KKAI_SHOP_HTTP_TEST_DIR`；不要将目录内容复制到仓库。命令中的密码在运行时随机生成，不会输出。

   ```bash
   set +x
   umask 077
   export KKAI_SHOP_HTTP_TEST_DIR="$(mktemp -d /tmp/kk-shop-http-XXXXXXXXXX)"
   chmod 700 "$KKAI_SHOP_HTTP_TEST_DIR"
   python3 - <<'PY'
   import json, os, pathlib, secrets, subprocess, time

   directory = pathlib.Path(os.environ['KKAI_SHOP_HTTP_TEST_DIR'])
   suffix = secrets.token_hex(5)
   container = 'kk-shop-http-' + suffix
   database = 'kkai_test_shop_http_' + suffix
   username = 'kkai_shop_http'
   password = secrets.token_hex(24)
   env_file = directory / 'postgres.env'
   env_file.write_text(
       f'POSTGRES_USER={username}\nPOSTGRES_PASSWORD={password}\nPOSTGRES_DB={database}\n'
   )
   env_file.chmod(0o600)
   subprocess.run([
       'docker', '--context', 'desktop-linux', 'run', '--detach', '--pull', 'never',
       '--name', container, '--env-file', str(env_file),
       '--publish', '127.0.0.1::5432', 'postgres:18.4-bookworm',
   ], check=True, stdout=subprocess.DEVNULL)
   ports = json.loads(subprocess.check_output([
       'docker', '--context', 'desktop-linux', 'inspect',
       '--format', '{{json .NetworkSettings.Ports}}', container,
   ], text=True))
   port = ports['5432/tcp'][0]['HostPort']
   runtime = {
       'container': container,
       'directory': str(directory),
       'dsn': f'postgres://{username}:{password}@127.0.0.1:{port}/{database}?sslmode=disable',
   }
   (directory / 'runtime.json').write_text(json.dumps(runtime))
   (directory / 'runtime.json').chmod(0o600)
   for attempt in range(60):
       ready = subprocess.run([
           'docker', '--context', 'desktop-linux', 'exec', container,
           'pg_isready', '-h', '127.0.0.1', '-U', username, '-d', database,
       ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
       if ready.returncode == 0:
           break
       time.sleep(0.5)
   else:
       raise SystemExit('Local PostgreSQL did not become ready; clean up this fixture.')
   print('Disposable PostgreSQL ready; private files:', directory)
   PY
   ```

   `--pull never` 不会下载镜像。若启动失败，处理本地前置条件后使用新目录重新开始；保留失败目录以便按第 5 步清理。

2. 从空库安装实际 canonical schema fixture。这里只读取刚生成的私有 DSN；fixture 会拒绝非 `127.0.0.1`、非 `kkai_test_shop_http_` 库名和非空数据库。

   ```bash
   export KKAI_SHOP_HTTP_TEST_DSN="$(python3 -c 'import json, os, pathlib; print(json.loads((pathlib.Path(os.environ["KKAI_SHOP_HTTP_TEST_DIR"]) / "runtime.json").read_text())["dsn"])')"
   export KKAI_SHOP_HTTP_FIXTURE_SCHEMA=19
   go test ./pkg/kkaimigrate -run '^TestShopHTTPFixtureSchema$' -count=1 -timeout=120s
   ```

   `KKAI_SHOP_HTTP_FIXTURE_SCHEMA` 默认为 19。可选的 18 只供独立 schema CLI 演练使用，不适用于接下来的商城 HTTP 验收。不要在同一个非空数据库重跑 schema fixture。

3. 启动实际 HTTP harness。它为虚构买家、管理员、商品和库存生成独立凭据及密钥，禁用邮件，创建本机支付模拟器，并运行实际后台任务。以下命令是常驻本地服务，不是全量测试；用第 5 步的 `STOP` 文件结束。

   ```bash
   go test ./router -run '^TestShopLiveHTTPServer$' -v -count=1 -timeout=0 \
     >"$KKAI_SHOP_HTTP_TEST_DIR/server.log" 2>&1 &
   printf '%s\n' "$!" >"$KKAI_SHOP_HTTP_TEST_DIR/server.pid"
   python3 - <<'PY'
   import os, pathlib, time
   directory = pathlib.Path(os.environ['KKAI_SHOP_HTTP_TEST_DIR'])
   for attempt in range(60):
       if (directory / 'READY').is_file():
           print('Actual HTTP fixture ready at http://localhost:3000')
           break
       time.sleep(0.5)
   else:
       raise SystemExit('Not ready; inspect the private server.log before continuing.')
   PY
   ```

   `credentials.json`、`server.log` 和运行记录只保留在私有目录，权限为 0600。harness 自行设置 `SHOP_SECRET_ACTIVE_KEY_ID`、`SHOP_SECRET_KEYS`、`SHOP_FINGERPRINT_ACTIVE_KEY_ID`、`SHOP_FINGERPRINT_KEYS`，并清空两个 `*_KEYS_FILE`，避免继承宿主配置。不要导入真实账号或商品。harness 每次启动会重新生成种子和密钥；需要重启时重新创建整套隔离数据库，不复用旧数据。

   浏览器验收可使用现有前端开发服务器 `http://localhost:5178`，代理指向 `http://localhost:3000`。支付模拟器使用自签 TLS，仅在隔离浏览器中允许它的本机证书。应用 Host 校验拒绝 IP 字面量，浏览器应使用 `localhost`。纯 HTTP 脚本不需要前端服务器，因为它们不会跟随支付后的浏览器跳转。

4. 执行两份实际 HTTP 验收脚本。每份通常在一分钟内完成，后台对账最长需要约 30 秒。它们仅接受私有目录、固定 localhost 后端和精确匹配的本机 TLS 网关，禁用代理及重定向，不打印凭据或密钥。

   ```bash
   python3 scripts/kkai/tests/shop-http-acceptance.py "$KKAI_SHOP_HTTP_TEST_DIR"
   python3 scripts/kkai/tests/shop-http-admin-acceptance.py "$KKAI_SHOP_HTTP_TEST_DIR"
   ```

   第一份覆盖登录/刷新、幂等下单、权限隔离、签名回调、自动交付、完整退款和 PostgreSQL 账务；第二份覆盖库存导入/去重及人工交付。结果分别写入私有目录的 `acceptance-result.json`、`admin-acceptance-result.json`。本次历史验收为 33 个断言（31 个不同检查），脱敏记录见 [real-http-review.json](kk-shop-screenshots/implementation/real-http-review.json)。整理后的完整业务链未再次执行；全量测试未运行。

   仅验证支付事件修复时，可在全新隔离 fixture 上执行下面的定向命令。它实际经过登录、下单、签名回调、review、对账及 worker，检查 review 阶段没有公开到账事件，确认付款后只有一份回执、一个公开到账事件和一次交付。结果写入 `payment-event-result.json`。

   ```bash
   python3 scripts/kkai/tests/shop-http-acceptance.py --payment-events-only "$KKAI_SHOP_HTTP_TEST_DIR"
   ```

   当前修复源码已在新目录、新数据库及新 HTTP 进程中通过上述 6 项检查，并用第 5 步的 `STOP` 验证正常退出。整个定向运行外层限制为 180 秒，实际约 59 秒。历史浏览器截图保留修复前时间线，不作为当前支付事件唯一性的证据。

5. 浏览器验收完成后先停止服务，再移除本次容器及匿名卷。必须保留仍需交接的脱敏结果后才删除私有目录。

   ```bash
   touch "$KKAI_SHOP_HTTP_TEST_DIR/STOP"
   python3 - <<'PY'
   import json, os, pathlib, re, shutil, subprocess, time

   directory = pathlib.Path(os.environ['KKAI_SHOP_HTTP_TEST_DIR']).resolve()
   if not directory.name.startswith('kk-shop-http-') or directory.parent not in {pathlib.Path('/tmp'), pathlib.Path('/private/tmp')}:
       raise SystemExit('Unexpected fixture directory; no cleanup performed.')
   pid_file = directory / 'server.pid'
   if pid_file.exists():
       pid = int(pid_file.read_text())
       for attempt in range(60):
           try:
               os.kill(pid, 0)
           except ProcessLookupError:
               break
           time.sleep(0.5)
       else:
           raise SystemExit('Harness is still running; inspect it before removing the database.')
   runtime_file = directory / 'runtime.json'
   if runtime_file.exists():
       runtime = json.loads(runtime_file.read_text())
       container = runtime['container']
       if not re.fullmatch(r'kk-shop-http-[a-f0-9]+', container):
           raise SystemExit('Unexpected container name; no container removed.')
       subprocess.run([
           'docker', '--context', 'desktop-linux', 'rm', '--force', '--volumes', container,
       ], check=True, stdout=subprocess.DEVNULL)
   shutil.rmtree(directory)
   print('Local HTTP fixture, container volume and private files removed.')
   PY
   unset KKAI_SHOP_HTTP_TEST_DSN KKAI_SHOP_HTTP_TEST_DIR KKAI_SHOP_HTTP_FIXTURE_SCHEMA
   ```

   若容器启动成功但尚未生成 `runtime.json` 即异常，按启动时唯一的 `kk-shop-http-<随机后缀>` 名称核对并单独删除该容器；不要执行全局 Docker prune。常驻服务的清理会先停止 worker、排空 HTTP，再恢复全局状态并关闭数据库连接。
