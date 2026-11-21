# mobilebot/tool

## 应用图标（MaclawGo）

- **源图**：`guiapp\build\appicon.png`（1024×1024，圆角透明外角 + 白色外边距，可见内容只占约 76%）。
- **生成命令**（在仓库根目录执行）：

  ```bat
  powershell -ExecutionPolicy Bypass -File mobilebot\tool\gen_app_icons.ps1
  powershell -ExecutionPolicy Bypass -File mobilebot\tool\gen_app_icons.ps1 -Check
  ```

  `-Check` 只校验不写文件：核对每个输出的像素尺寸，以及深青外框占图标边长的比例（88%～98%：
  低于 88% 说明白边太宽、图标又变小了，高于 98% 说明外框被裁掉了），不达标时返回 1。
  可选 `-Source <png>` 换源图。

- **做法**：白边用源图自身的白（250,252,252）填，不从透明开始；按 alpha>10 求可见包围盒 → 居中裁成正方形并四周各留 4% 边距（正好是"外侧一圈细白边"）
  → 高质量缩放（HighQualityBicubic + TileFlipXY 防边缘发虚）。

- **外观**：深青色外框贴着图标边缘，外侧只留一圈约 4% 的白边，白边外再清成 24.5% 圆角，使启动器自己的
  圆角遮罩边缘可见；不要裁成硬方形（见下面 Android 一节）。

- **Android**：输出 `mobilebot\android\app\src\main\res\mipmap-{mdpi,hdpi,xhdpi,xxhdpi,xxxhdpi}\ic_launcher.png`
  （48/72/96/144/192），保留透明通道；为什么加圆角遮罩：源图本身带 22% 左右的圆角，直接正方形输出会在
  方角处露出白底，桌面启动器再按自己的形状二次裁切时会出现白边。遮罩半径取边长的 24.5%，比启动器遮罩略大，所以外面能看到一圈白边和启动器自己的圆角。

- **iOS**：输出 `mobilebot\ios\Runner\Assets.xcassets\AppIcon.appiconset\` 下 Contents.json 声明的全部 15 个
  尺寸（20@1x/2x/3x、29@1x/2x/3x、40@1x/2x/3x、60@2x/3x、76@1x/2x、83.5@2x、1024@1x），必须无 alpha
  （App Store 拒收带透明通道的图标），所以统一铺白底后输出 24bpp。

- **约束**：`AndroidManifest.xml` 仍指向 `@mipmap/ic_launcher`，没有引入 adaptive icon（`mipmap-anydpi-v26`），
  换图标时不需要改清单，也不要手改 PNG —— 改源图后重跑脚本。
