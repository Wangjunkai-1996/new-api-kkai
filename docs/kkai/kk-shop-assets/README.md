# KK 商城原创纸艺素材包

制作日期：2026-10-27。供应方：KKAI / KKRICH；模型：`gpt-image-2`。
通过用户指定的 `kkrich-image` 技能生成；每张独立生成一次，共 9 张，无自动重试。

[查看整套视觉联系图](./art-board.jpg) · [美术决策](../kk-shop-art-direction.md) · [完整生成提示词](./generation-prompts.json) · [尺寸与资产清单](./asset-manifest.json)

## 素材与尺寸

| 用途 | 页面素材 | 请求 / 实际像素 | 页面文件大小 |
| --- | --- | --- | --- |
| 首页主视觉 | [hero.webp](./hero.webp) | 1536x1024 / 1536x1024 | 44.9 KB |
| 灵感工作台 · 独立账号 | [product-1.webp](./product-1.webp) | 1024x1024 / 1024x1024 | 48.8 KB |
| 轻记云 · 兑换卡密 | [product-2.webp](./product-2.webp) | 1024x1024 / 1024x1024 | 34.0 KB |
| 个人工具箱 · 开通服务 | [product-3.webp](./product-3.webp) | 1024x1024 / 1024x1024 | 49.8 KB |
| 云端书房 · 阅读账号 | [product-4.webp](./product-4.webp) | 1024x1024 / 1024x1024 | 48.0 KB |
| 拾光素材 · 下载卡密 | [product-5.webp](./product-5.webp) | 1024x1024 / 1024x1024 | 68.9 KB |
| 效率空间 · 设置服务 | [product-6.webp](./product-6.webp) | 1024x1024 / 1024x1024 | 35.4 KB |
| 购买与交付指南 | [service-guide.webp](./service-guide.webp) | 1536x1024 / 1536x1024 | 45.1 KB |
| 普通空列表 | [empty-state.webp](./empty-state.webp) | 1024x1024 / 1024x1024 | 31.9 KB |

九张返回尺寸与请求一致。原始 PNG 共 10.58 MiB，保存在 `originals/`。页面使用的全部 22 个 WebP（包括详情和缩略派生图）共 824.4 KiB，不会在首页同时加载全部派生图。

## 切图与接入

- `product-N.webp`：完整 1024×1024 商品概念图。
- `product-N-thumb.webp`：320×320，用于结账、订单和管理列表。
- `product-N-detail.webp`：由原图中央 64% 区域裁切、输出 900×900，作为纸艺细节，不是真实软件界面或额外商品权益。
- `hero-mobile.webp`：900×600 的轻量移动版，保留完整构图；页面再按真实可用空间布局。
- `hero.webp` 与 `service-guide.webp`：1536×1024；`empty-state.webp`：1024×1024。

WebP 使用 Pillow 编码，主图质量 86、缩略图 82、移动 Hero 84。保留母图以便后续重新切图，不需要再次付费生成。
所有图采用纸艺数字隐喻，不是物理商品照片。商品名称、类型、规格、价格、库存和真实状态必须在图片外以可读取文字展示；价格与演示业务条件不能烘焙进图片。

普通空载图只用于无结果/无订单；付款差错、退款失败及风险暂停应直接展示准确说明。图片不代表第三方品牌授权、安全认证或付款成功。
