# Android 构建与 relay 发布

客户端 `cmd/t3go-app` 使用 Asuka 构建。Android 产物是 AAB；分发给手机时再用
`bundletool` 生成 universal APK。

## 构建

```sh
export JAVA_HOME=/Library/Java/JavaVirtualMachines/jdk-17.jdk/Contents/Home
export ANDROID_HOME="$HOME/Library/Android/sdk"
export PATH="$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$PATH"
export ASUKA_KEYSTORE="$HOME/.config/t3go/android/upload.jks"
export ASUKA_KEY_ALIAS=upload
export ASUKA_KEYSTORE_PASSWORD="$(cat "$HOME/.config/t3go/android/upload.password")"

go test ./... -count=1
go vet ./...
go tool asuka vet ./cmd/t3go-app
./scripts/build-web.sh
go tool asuka build --platform android ./cmd/t3go-app
```

`dist/` 中会生成 `T3Go.aab`、`T3Go-mapping.txt` 和 `dist/web/`。

## universal APK

```sh
PASS="$(cat "$HOME/.config/t3go/android/upload.password")"
java -jar bundletool-all.jar build-apks \
  --bundle=cmd/t3go-app/dist/T3Go.aab \
  --output=/tmp/T3Go.apks --mode=universal \
  --ks="$HOME/.config/t3go/android/upload.jks" \
  --ks-key-alias=upload \
  --ks-pass=pass:"$PASS" --key-pass=pass:"$PASS"
unzip -o /tmp/T3Go.apks universal.apk -d /tmp/t3go-apk
cp /tmp/t3go-apk/universal.apk cmd/t3go-app/dist/T3Go-universal.apk
shasum -a 256 cmd/t3go-app/dist/T3Go-universal.apk \
  > cmd/t3go-app/dist/T3Go-universal.apk.sha256
apksigner verify --verbose cmd/t3go-app/dist/T3Go-universal.apk
```

通用 APK 含全部 ABI，通常超过 relay 附件接口 25 MB 的上限。需要单文件上传时，
可保留 arm64 并重新对齐、签名：

```sh
cp cmd/t3go-app/dist/T3Go-universal.apk /tmp/T3Go-arm64.apk
zip -d /tmp/T3Go-arm64.apk 'lib/x86/*' 'lib/x86_64/*' 'lib/armeabi-v7a/*'
zipalign -f -p 4 /tmp/T3Go-arm64.apk /tmp/T3Go-arm64-aligned.apk
apksigner sign --ks "$HOME/.config/t3go/android/upload.jks" \
  --ks-key-alias=upload --ks-pass=pass:"$PASS" --key-pass=pass:"$PASS" \
  --out cmd/t3go-app/dist/T3Go-arm64.apk /tmp/T3Go-arm64-aligned.apk
apksigner verify --verbose cmd/t3go-app/dist/T3Go-arm64.apk
```

没有 SSH 权限时，也可用环境 Token 把 APK 和安装页上传到
`POST /api/v1/uploads?name=...`；下载地址是
`/api/v1/uploads/{id}?token=...`，并通过 relay 的 `/e/{environmentId}/`
前缀访问。

## 上传到 relay

relay 服务器把 Web 与 APK 放在同一个 `--web` 目录。上传后重启服务端二进制
即可同时更新 Web、API 和下载地址：

```sh
tar -C cmd/t3go-app/dist/web -czf /tmp/t3go-web.tgz .
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' \
  -o /tmp/t3go-linux ./cmd/t3go

scp /tmp/t3go-linux /tmp/t3go-web.tgz \
  cmd/t3go-app/dist/T3Go-universal.apk \
  cmd/t3go-app/dist/T3Go-universal.apk.sha256 \
  ubuntu@RELAY_HOST:/tmp/

ssh ubuntu@RELAY_HOST '
  cd /opt/t3go/web &&
  tar -xzf /tmp/t3go-web.tgz &&
  cp /tmp/T3Go-universal.apk /tmp/T3Go-universal.apk.sha256 /opt/t3go/web/ &&
  chmod 644 /opt/t3go/web/T3Go-universal.apk* &&
  sudo install -m 755 /tmp/t3go-linux /opt/t3go/t3go &&
  sudo systemctl restart t3go'
```

验证：

```sh
curl -sI http://RELAY_HOST:4870/app.wasm | grep -i content-type
curl -s  http://RELAY_HOST:4870/T3Go-universal.apk.sha256
```

`/app.wasm` 必须是 `application/wasm`，否则浏览器会退化为慢速流式编译。

## 配对链接

环境电脑保存 `relay.json` 后，生成供客户端粘贴的 relay 配对链接：

```sh
t3go pair --home /path/to/environment-data --relay
```

链接形如：

```text
t3go://pair?url=http://RELAY_HOST:4870/e/ENVIRONMENT&token=...&environment=ENVIRONMENT&key=ENVIRONMENT&direct=...
```

把整条链接粘贴到客户端连接页即可。`direct` 是可选局域网直连地址；离开局域网时
客户端会自动使用 relay。
