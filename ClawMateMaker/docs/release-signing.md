# 固件发布签名与桌面信任锚

ClawMate Maker 只安装 `manifest.sig` 能由内置 Ed25519 公钥验证的 `.clawfw`。私钥只存在于受保护的 GitHub Release 环境；任何开发机、Release ZIP 或前端输入均不能提供或替换信任根。

发布前一次性配置：

1. 生成 Ed25519 密钥对；将私钥 Base64 存为环境级 secret `CLAWMATE_FIRMWARE_SIGNING_KEY`。
2. 将同一密钥的 `keyId` 写入环境级 variable `CLAWMATE_FIRMWARE_SIGNING_KEY_ID`。
3. 将公钥 Base64 存为环境级 variable `CLAWMATE_FIRMWARE_PUBLIC_KEY`。
4. 桌面构建使用以下 linker 参数注入公钥与 key ID：

```text
-X main.releaseKeyID=$CLAWMATE_FIRMWARE_SIGNING_KEY_ID
-X main.releasePublicKeyBase64=$CLAWMATE_FIRMWARE_PUBLIC_KEY
```

桌面开发构建中的公钥为空，因此对任何网络下载包均会拒绝安装；这是预期的 fail-closed 行为。要轮换密钥，先发布包含新旧公钥的桌面版本，再切换固件签名密钥，最后移除旧公钥。
