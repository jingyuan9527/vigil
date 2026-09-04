# DockMon 界面设计规范（UI Design Specification）

> 版本：v1.1 ｜ 适用范围：DockMon 前端（React 18 + React Router 6 + Tailwind 3.4 + Vite 5）
> 风格基准：**Bento Grid（便当盒布局）** —— 以 `stylekit.top/bento-grid` 官方风格引用为权威风格层，本项目既有 `index.css`/组件已基本实现该风格，本规范据此提炼为统一、可落地、可复用、且**兼容移动端**的设计系统。
> 目标：运维 / SRE / 开发者单人或小团队自托管场景；一眼看清「哪些镜像该升级」，操作可信赖、反馈即时。

---

## 0. 总则与风格冲突仲裁

### 0.1 风格优先级
1. **Bento Grid 官方引用（Hard Prompt）为最高优先级**，禁止项（FORBIDDEN）与必含项（REQUIRED）必须逐条满足。
2. 官方引用内部存在两处自相矛盾，按下表**仲裁**后执行（工程以本表为准）：

| 冲突点 | 官方两说 | 仲裁结论（项目落地） |
|---|---|---|
| 卡片 hover 位移 | Token 字典：`hover:-translate-y-0.5`（无缩放）；Widget-Feel/示例：`-translate-y-1 scale-[1.01]` | **采用强表达版** `-translate-y-1 scale-[1.01]` + 宽阴影，与现有 `index.css` 一致；`-translate-y-0.5` 仅作「密集列表行」轻量变体 |
| 蓝紫渐变 | 配色建议用 `from-blue-500 to-purple-600`；自检清单写「没有紫色到蓝色的渐变」 | `blue→purple` 是**唯一批准的主渐变**（Hero/品牌）；清单意为「勿将其散用于各处小元件」；其余强调卡用 `orange→pink` / `green→cyan` |
| 栅格列数 | Hard Prompt：`grid-cols-4`；骨架模板：`lg:grid-cols-3` | 桌面 **4 列**（`grid-cols-4`）为基准；平板 2 列；手机 1 列（与现有 `.bento-grid` 一致） |

### 0.2 设计原则（强制）
1. **一致性优先**：全局复用同一套令牌 / 组件 / 页头 / 工具栏模式；新增样式先复用令牌，禁止页面内硬编码颜色。
2. **状态双通道**：状态必用「色块/圆点 + 文案」同时表达，不依赖颜色 alone（色盲 + WCAG）。
3. **Bento 特色不可丢**：禁止所有卡片等大；卡片须跨行/跨列制造层次；大卡放主内容、小卡放次要信息。
4. **操作可预期**：写操作（忽略/移除/保存/测试）必须有 loading/success/error 三态；破坏性操作走 ConfirmDialog。
5. **明暗等价 + 移动等价**：深色为令牌反转；移动端为同一信息架构的降密度重排，不丢功能。
6. **禁止裸直角与圆角过小**：一律 `rounded-xl`(12px) / `rounded-2xl`(16px)，禁用 `rounded-none` / `rounded-sm`。

### 0.3 文档约定
- 视觉令牌以 **Tailwind 工具类 + CSS 变量**双形式给出。
- `[API]` 标注字段直接来自 `frontend/src/api/client.js` 响应体。
- 强制一致性规则（A~K）与风格自检清单（§6）为不可协商项，代码评审据此检查。

---

## 1. 设计系统基础（Design Tokens）

### 1.1 色彩系统

#### 1.1.1 中性骨架（Neutral / Zinc）
| 用途 | Light | Dark |
|---|---|---|
| 页面背景 | `bg-zinc-50` (#fafafa) | `bg-zinc-950` (#09090b) |
| 卡片背景 | `bg-white` (#fff) | `bg-zinc-900` (#18181b) |
| 次级背景/输入 | `bg-zinc-100` | `bg-zinc-800` |
| 主文字 | `text-zinc-900` | `text-zinc-100` |
| 次级文字 | `text-zinc-600` | `text-zinc-400` |
| 弱化文字 | `text-zinc-400` | `text-zinc-500` |
| 边框 | `border-zinc-100`（卡片）/ `border-zinc-200`（输入） | `border-zinc-800` / `border-zinc-700` |
| 悬停底色 | `hover:bg-zinc-100` | `hover:bg-zinc-800` |

#### 1.1.2 品牌与主渐变（强制规则：仅此一套主渐变）
- **Hero / 品牌主渐变**：`bg-gradient-to-br from-blue-500 to-purple-600`，白字。
- **其余强调渐变（任选，用于 StatCard 图标容器 / 特色卡）**：
  - `from-orange-400 to-pink-500`（有更新 / 警示）
  - `from-green-400 to-cyan-500`（已是最新 / 健康）
  - `from-blue-500 to-violet-600`（信息 / 可选更新）
- **禁止**：在上述三套外新增任意渐变；把主蓝紫渐变散用到按钮、徽章、列表行等小元件。

#### 1.1.3 语义色
| 语义 | Light 实色 | Dark 实色 | 用途 |
|---|---|---|---|
| success | `emerald-500 #10b981` | `emerald-400` | 已是最新、保存成功 |
| warning | `amber-500 #f59e0b` | `amber-400` | 有更新（强提醒） |
| danger | `rose-500 #f43f5e` | `rose-400` | 缺失 / 移除 / 错误 |
| info | `blue-500` | `blue-400` | 可选更新（new-tag） |

#### 1.1.4 状态色映射（强制一致性规则 A，状态色板唯一）
> 全局唯一。**所有页面**的状态徽章、圆点、分布条、时间线节点必须严格使用下表，禁止新增/替换色值；徽章为「浅底 + 实色字」，**禁止在彩色底上放灰色文字**。

| key | 中文 | 圆点/实色 | 浅色徽章 | 深色徽章 | 位置 |
|---|---|---|---|---|---|
| `up-to-date` | 已是最新 | `emerald-500` | `bg-emerald-50 text-emerald-700` | `bg-emerald-500/15 text-emerald-300` | 全局 |
| `update-available` | 有更新 | `amber-500` | `bg-amber-50 text-amber-700` | `bg-amber-500/15 text-amber-300` | 全局 |
| `new-tag` | 可选更新 | `blue-500` | `bg-blue-50 text-blue-700` | `bg-blue-500/15 text-blue-300` | Notifications |
| `unknown` | 未知 | `zinc-400` | `bg-zinc-100 text-zinc-600` | `bg-zinc-700/40 text-zinc-300` | 全局 |
| `stale` | 缺失/已清理 | `rose-500` | `bg-rose-50 text-rose-700` | `bg-rose-500/15 text-rose-300` | Images/Dashboard |
| `ignored` | 已忽略（标记） | `zinc-400` | `bg-zinc-100 text-zinc-500` | `bg-zinc-700/40 text-zinc-400` | Images |

#### 1.1.5 对比度（WCAG AA）
- 正文 `zinc-900/100` ≥ 7:1（AAA）；徽章文字（`amber-700` on `amber-50` 等）≥ 4.5:1。
- 彩色背景（Hero 渐变）上只用 `text-white` / `text-white/80`，**禁止** `text-zinc-400/500` 等灰字。
- 禁用浅色背景下用 `amber-400` 作文字（仅图标实色 / 深色可用）。

### 1.2 字体排版（强制一致性规则 B）
- **字族**：`"Plus Jakarta Sans", ui-sans-serif, system-ui, sans-serif`（UI/正文，**非** Inter/Roboto/Geist，符合风格禁用清单）；`"JetBrains Mono", ui-monospace`（digest/tag/版本号）。
- **字阶**：

| Token | 大小/行高 | 字重 | 用途 |
|---|---|---|---|
| Hero | 30/36 → (md)36/40 | 800 | 仅 Hero 主标题 |
| H1 | 24/32 → md:30 | 700 | 页面主标题（每页 1 个） |
| H2 | 20/28 → md:24 | 600 | 区块/卡片标题 |
| H3 | 16/24 | 600 | 卡片内小标题 |
| body | 14/22 → md:16 | 400/500 | 正文/列表 |
| sm | 13/20 | 400 | 按钮/辅助 |
| caption | 12/18 | 500 | 徽章/时间戳/标签 |
| mono | 13 | 400 | digest/tag |

- 标题 `tracking-tight`；数字统计用 `tabular-nums`。
- **禁止**：tiny uppercase tracked eyebrow 放在每个 section 标题上方。

### 1.3 间距系统（强制一致性规则 C —— 间隙禁令）
- **禁止间隙**：`gap-1`、`gap-2`、`gap-10`、`gap-12` 一律禁用（风格 FORBIDDEN）。
- **最小间隙**：统一 **`gap-3`（12px）**；图标与文字间距用 `gap-3`（极紧凑可用 `gap-2.5`，但推荐 `gap-3`）。
- **栅格间隙**：Bento 网格统一 `gap-4` 或 `gap-6`（推荐 `gap-4` md、`gap-6` lg 更透气）；整页区块 `space-y-6`。
- **卡片内边距**：`p-4 md:p-6`。
- 4px 基准刻度：`sp-1=4 sp-2=8 sp-3=12 sp-4=16 sp-6=24 sp-8=32 sp-12=48 sp-16=64`。

### 1.4 圆角 / 阴影 / 层级（强制一致性规则 D）
- **圆角**：卡片 `rounded-2xl`(16)；按钮/输入/徽章/小卡 `rounded-xl`(12)；胶囊 `rounded-full`。**禁用 `rounded-none`/`rounded-sm`**（风格 FORBIDDEN）。
- **阴影（Bento 卡片）**：静止 `shadow-sm`；hover 展开至 `shadow-[0_8px_30px_rgba(0,0,0,0.08)]`（深：`rgba(0,0,0,.45)`）；保留 `shadow-md` 作过渡。
- **按下反馈**：`active:scale-95`（模拟物理按压）。
- **层级**：Sidebar/Topbar `z-30`；底部 Tab 栏 `z-30`；ConfirmDialog `z-50`；Toast `z-[60]`。

### 1.5 栅格与布局（Bento 核心，强制一致性规则 E）
- **桌面 4 列**：`grid grid-cols-4 gap-4 lg:gap-6`（Hard Prompt 基准）。
- **跨格**：大卡 `col-span-2 row-span-2`；中卡 `col-span-2` 或 `row-span-2`；小卡 `1x1`。每屏至少 1 个大卡 + 2~3 中卡，避免全等大。
- **响应式降级**：≥1024 四列；640–1023 两列（`grid-cols-2`，跨格降级为 1 列）；<640 单列（`grid-cols-1`）。
- **比例**：大卡可用 `aspect-ratio` 维持比例（如 Hero `aspect-[4/3]` md 起），避免内容撑爆。
- **内容容器**：`mx-auto max-w-[1200px] px-4 md:px-6 lg:px-8`。

### 1.6 动效（强制一致性规则 F）
- **缓动**：`ease-out`（**禁用** `bounce`/`elastic`/弹簧过冲）；时长 `duration-200`~`duration-300`。
- **Bento 卡片 hover（强表达版，项目既定）**：`hover:-translate-y-1 hover:scale-[1.01]` + 阴影由 `shadow-sm`→宽阴影；`transition-all duration-200`。
- **密集列表行 hover（轻量变体）**：`hover:-translate-y-0.5 hover:shadow-md`（通知行、镜像项在非网格视图）。
- **卡片内图标**：`group-hover` 时独立 `scale-110` + 变色（bg swap）。
- **输入框聚焦**：`transition-all duration-200` + `focus:ring-2 focus:ring-blue-500/20`。
- **强制**：所有位移/缩放动效包裹 `@media (prefers-reduced-motion: reduce)` 关闭位移与旋转，仅保留透明度。

---

## 2. 通用组件库（Component Library）

> 全部基于第 1 章令牌；下列「REQUIRED 类串」直接取自官方风格，落地时**原样包含**。

### 2.0 官方 REQUIRED 类串（必含，自检用）
- **按钮**：`px-4 py-2 md:px-6 md:py-3 rounded-xl font-medium transition-colors` + 配色。
- **卡片**：`rounded-2xl border border-zinc-100 dark:border-zinc-800 shadow-sm hover:shadow-md hover:-translate-y-0.5 transition-all duration-200 p-4 md:p-6`（注：项目采用更强 hover `-translate-y-1 scale-[1.01]`，见 §1.6 仲裁；二者取强表达版）。
- **输入框**：`bg-zinc-50 dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-500 transition-all`。
- **官方 FORBIDDEN 类**：`rounded-none` `rounded-sm` `gap-1` `gap-2` `gap-10` `gap-12` → 出现即重写。

### 2.1 按钮 Button
- **变体**：
  - `primary`：`bg-zinc-900 text-white`（深 `bg-white text-zinc-900`），hover `bg-zinc-700`/`bg-zinc-200`；每页唯一主行动。
  - `accent`：`bg-gradient-to-br from-blue-500 to-purple-600 text-white`，仅品牌强引导（首次设置）。
  - `secondary`：`border border-zinc-200 text-zinc-700 hover:bg-zinc-50`（深 `border-zinc-700 text-zinc-100 hover:bg-zinc-800`）。
  - `danger`：`bg-rose-500 text-white hover:bg-rose-600`，仅确认态。
  - `ghost`：无边框无底，hover 底色，图标按钮。
- **尺寸**：`md`(默认, 上面的 px-4 py-2 md:px-6 md:py-3) / `sm`(`h-9 px-3`) / `lg`(`h-11 px-6`)；移动端建议 `md:` 尺寸即全宽或加大触控。
- **状态**：`disabled`→`opacity-60 cursor-not-allowed`；`loading`→内嵌 spinner+disabled；`active:scale-95`。

### 2.2 输入框 Input / 文本域
- 必含类串见 2.0；占位 `placeholder-zinc-400`；错误态 `border-rose-400` + 下方 `text-rose-500 text-sm`；数字输入 `text-right tabular-nums`。

### 2.3 开关 Switch
- `h-6 w-11 rounded-full`，开 `bg-blue-600`/关 `bg-zinc-300`（深 `bg-zinc-700`）；滑块 `h-5 w-5 translate-x-5/translate-x-0.5`；`role="switch" aria-checked`；标签左、开关右基线对齐。

### 2.4 徽章 / 状态徽章 StatusBadge
- 输入 `status` key，按 §1.1.4 输出「圆点 + 文案」胶囊（`rounded-xl px-2.5 py-1 text-caption`）。
- 普通标签：`bg-zinc-100 text-zinc-600`（深 `bg-zinc-800 text-zinc-300`）`rounded-lg px-2 py-0.5 text-caption font-mono`。
- 未读小圆点：`h-2 w-2 rounded-full bg-amber-500`。

### 2.5 卡片 BentoCard（**禁止嵌套卡片**，强制一致性规则 G）
- 容器：`rounded-2xl border border-zinc-100 dark:border-zinc-800 bg-white dark:bg-zinc-900 shadow-sm` + hover（强表达版）+ `transition-all duration-200 p-4 md:p-6`；`min-w-0` 防溢出。
- 支持 `span`：`lg`(2×2) / `wide`(2×1) / `tall`(1×2) / 默认(1×1)。
- **禁止卡片内再放 `rounded-2xl + border + shadow` 的子卡片**（风格清单「没有嵌套卡片」）。卡片内列表项用**轻量行**：`rounded-xl p-3 hover:bg-zinc-50 dark:hover:bg-zinc-800/50`（无边框无阴影）；分隔用 `divide-y divide-zinc-100 dark:divide-zinc-800`。

### 2.6 统计卡 StatCard
- 结构：左上标签（text-sm 次级）+ 大数字（text-3xl font-bold tabular-nums）+ 可选 hint；右上渐变图标容器（`h-11 w-11 rounded-2xl bg-gradient-to-br <强调渐变> text-white`，图标 `group-hover:scale-110`）。

### 2.7 分页 Pagination
- 页码按钮 `h-9 min-w-9 rounded-xl text-sm`；当前 `bg-zinc-900 text-white`（深 `bg-white text-zinc-900`）；ghost `text-zinc-500 hover:bg-zinc-100`；总数提示 `text-caption text-zinc-400` 居左、翻页居右；`total<=pageSize` 返回 null。

### 2.8 空状态 EmptyState
- 居中图标容器（`h-12 w-12 rounded-2xl bg-zinc-100 text-zinc-400` 深 `bg-zinc-800`）+ 主文案（text-body 次级）+ 可选行动按钮。文案见各页。

### 2.9 加载 Spinner / 骨架
- 行内 Spinner（沿用）居中 `py-16`，`border-zinc-300 border-t-blue-500`；页面级先 Spinner；列表可先骨架卡（BentoCard 占位 + `animate-pulse`）；扫描中按钮 `animate-spin`。

### 2.10 确认对话框 ConfirmDialog（统一移除流程）
- `固定遮罩 bg-black/40` + 居中卡 `rounded-2xl bg-white p-6 dark:bg-zinc-900 shadow-[0_8px_30px_rgba(0,0,0,0.08)] max-w-sm`；标题 + 说明 + `danger`「确认移除」+ `secondary`「取消」；`role="dialog" aria-modal`；`Esc` 关、`Enter` 主操作；移动端改**底部抽屉**（bottom sheet，`fixed bottom-0 inset-x-0 rounded-t-2xl`）。

### 2.11 Toast 轻提示
- 右上（桌面）/ 顶部居中（移动）`z-[60]`，`rounded-xl px-4 py-2.5 text-sm shadow-[0_8px_30px_rgba(0,0,0,0.08)]`；success(`emerald`)/error(`rose`)/info(`blue`)，`role="status"/"alert"`；3s 自动消失，最多叠 3 条。

### 2.12 来源标识 SourceTag / 标签组
- `source==='docker'` → 蓝点 +「Docker」；`watch`/`manual` → 灰点 +「手动」；`ignored` 标记 `bg-zinc-200 text-zinc-500`（深 `bg-zinc-700 text-zinc-400`）`rounded-md px-1.5 py-0.5 text-caption`。

---

## 3. 应用外壳与导航逻辑（App Shell）

### 3.1 桌面（≥lg）布局
```
┌──────────┬──────────────────────────────────┐
│ Sidebar  │ Topbar（最近扫描 + 扫描/主题）      │
│ 256px    ├──────────────────────────────────┤
│ Logo     │ 内容（max-w-1200 容器，可滚动）     │
│ Nav×5    │                                   │
│ 主题/退出 │                                   │
└──────────┴──────────────────────────────────┘
```
- 整体 `flex min-h-screen`；Sidebar `lg:w-64` 固定 `bg-white dark:bg-zinc-900 border-r border-zinc-100 dark:border-zinc-800`；Main `flex-1 overflow-y-auto`。
- 顶栏卡片 `rounded-2xl border bg-white px-4 py-3 dark:bg-zinc-900` 同宽容器内；左「最近扫描：{fmtTime}」+ 状态摘要，右 `主题切换`(ghost) + `立即扫描`(primary)。

### 3.2 导航项（强制一致性规则 H）
| 路由 | 名称 | 图标 | |
|---|---|---|---|
| `/` | 仪表盘 | Grid | `end` |
| `/images` | 镜像列表 | Box | |
| `/compare` | 版本对比 | Git | |
| `/notifications` | 更新通知 | Bell | 未读角标 |
| `/settings` | 设置 | Gear | |
- 激活：`bg-zinc-900 text-white`（深反之）；非激活 `text-zinc-600 hover:bg-zinc-100`。
- `<lg` 改用**底部 5 项 Tab 栏**（见 §4）；`*` 兜底 Dashboard。

### 3.3 路由守卫（沿用 App.jsx）
- `isAuthenticated` 假 → `Login`；否则 `Layout` 包裹；深层链接（`/compare?id=`）由页内 `useSearchParams` 回填。

---

## 4. 移动端设计规范（强制兼容，用户重点要求）

### 4.0 移动端原则
- **同一信息架构、降密度重排**：功能入口不丢，仅重排与合并。
- **断点**：手机 `<640`(sm) / 平板 `640–1023`(md~lg) / 桌面 `≥1024`(lg)。底部 Tab 栏在 **`<lg`（手机+平板）** 启用；`≥lg` 用左侧栏。
- **禁止横向溢出**：所有栅格/工具栏在 320px 宽不出现横向滚动（除明确横向滚动的 chip 行）。
- **触控目标 ≥ 44×44px**（WCAG + 风格清单）。

### 4.1 底部 Tab 栏（<lg 启用，强制一致性规则 I）
- 容器：`fixed bottom-0 inset-x-0 z-30 bg-white dark:bg-zinc-900 border-t border-zinc-100 dark:border-zinc-800`，底部加 `pb-[env(safe-area-inset-bottom)]`（iPhone 安全区）。
- 内容：`flex items-stretch justify-around`，5 项各 `flex-1`；每项 `flex flex-col items-center justify-center gap-1 py-2 min-h-[56px]`，图标 20px + 2 字标签 `text-caption`。
- 激活态：图标+文字 `text-blue-600 dark:text-blue-400`；非激活 `text-zinc-400 dark:text-zinc-500`；可选顶部 2px 指示条（**禁止单侧粗边框装饰**，用细高亮即可）。
- 内容区在 `<lg` 加 `pb-24` 避免被 Tab 栏遮挡。

### 4.2 顶部精简条（<lg）
- 品牌（左）+ `扫描`图标按钮 + `主题`图标按钮（右），`h-14 border-b`；不再放侧栏。
- `扫描中` 时图标 `animate-spin` + 文案「扫描中…」；角标未读同原逻辑。

### 4.3 Bento 栅格移动端
- `<640`：`grid-cols-1`（单列），所有 `span` 降级为整行；`gap-4`。
- `640–1023`：`grid-cols-2`，`lg/wide/tall` 跨格降级为 1 列（沿用现有媒体查询）。
- Hero 在手机用 `aspect-[4/3]` 或自适应高度，避免过大留白。

### 4.4 各页移动端适配要点
| 页 | 移动端要点 |
|---|---|
| Login | 同桌面居中；背景装饰降强度；输入全宽 |
| Dashboard | 单列：Hero→Stat(2 列)→分布(整行)→动态(整行)→扫描(整行)；数字 `tabular-nums` |
| Images | 单列卡；筛选 chips **横向滚动**（`overflow-x-auto` + `gap-3`，不换行）；搜索全宽；操作按钮组窄屏竖排 `gap-3` |
| Compare | 选择器改**顶部横向滚动 chip 行**（选中高亮），详情在下方整宽；时间线纵向滚动 |
| Notifications | 概览卡 2 列降级为整行堆叠；通知行 `flex-wrap` 防溢出；标记已读按钮满宽 |
| Settings | 全部单列；开关卡保持「左文右开关」不换行；输入全宽；保存/扫描按钮可满宽 |

### 4.5 触控 / 手势 / 键盘
- 所有可点元素 ≥44px；列表项整行可点。
- 确认对话框移动端为底部抽屉，下滑/点遮罩关闭。
- 键盘可达：Tab 顺序符合视觉；焦点环 `focus-visible:ring-2 ring-blue-500`；图标按钮 `aria-label`。

### 4.6 移动端禁止项
- 禁止把桌面侧栏简单压缩（必须用底部 Tab）。
- 禁止横向溢出（除声明式横向 chip 行）。
- 禁止触控目标 <44px。
- 禁止在 `<lg` 仍显示侧栏（冲突）。

---

## 5. 逐页设计规范

> 每页：目标 / 信息架构 / 布局结构 / 核心组件 / 交互 / 状态 / 响应式 / [API] 绑定。

### 5.1 登录 / 首次设置 `/`
- **目标**：首建管理员或登录；单屏无外壳。
- **布局**：背景模糊渐变装饰 + 居中 `max-w-sm` 卡 `rounded-2xl border bg-white/80 backdrop-blur`（*例外*：仅鉴权屏可用轻玻璃，不作内容卡默认）；Logo 64px 渐变容器 + 标题 + 副标题 + 用户名/密码输入 + 错误条 + 主按钮。
- **交互**：`required`；首次 `minLength=6`；按钮 `disabled` 当空/loading；提交 `setupRequired?authSetup:authLogin`→`login(token)`；失败错误条文案。
- **状态**：加载全屏 Spinner → 表单 → 错误条。
- **[API]**：`authCheck()`→`setup_required`；`authSetup/login`→`token`。
- **移动**：同布局，padding 收窄，玻璃仅此一处。

### 5.2 仪表盘 `/`
- **目标**：一屏掌握全局健康度与待办。
- **Bento（桌面 4 列，混合尺寸，满足「非全等」）**：
```
[ Hero(lg 2x2,渐变) ][ Stat:总数 ][ Stat:更新 ]
[ Hero              ][ Stat:最新 ][ Stat:未读 ]
[ 状态分布(wide).......... ][ 最近动态(tall 1x2) ]
[ 扫描信息 ][ 扫描信息 ]     [ 最近动态          ]
```
  - Hero：`aspect-[4/3]` md 起；系统状态 + 三 Mini（白底 `bg-white/10` 圆角，非嵌套卡）+ 渐变 CTA 引导。
  - 状态分布(wide)：占比条（emerald/amber/zinc，`gap-0` 连续段）+ 图例。
  - 最近动态(tall)：**轻量行列表**（`rounded-xl p-3 hover:bg-zinc-50`，无边框阴影），每行 `reference` + digest 变化 + StatusBadge；「查看全部」→`/notifications`。
- **交互**：占比条 hover 显数值；动态优先 `update-available` 5 条 + 通知 3 条；数字 `tabular-nums`。
- **状态**：loading→Spinner；空→Hero 提示「尚未添加镜像」+ EmptyState 引导去 `/images`。
- **[API]**：`stats()`→`{total,up_to_date,update_available,unknown,unread_notifications,last_scan_at,last_scan_status}`；`images()`；`notifications(false)`。
- **移动**：§4.4 单列顺序；Hero 自适应高。

### 5.3 镜像列表 `/images`
- **目标**：浏览/搜索/筛选，执行忽略、移除、进对比。
- **布局**：
  - 页头（H1+副标题 左 / 新增表单 右）。
  - 工具栏：筛选 chips（全部/有更新/已是最新/已忽略/未知）+ 搜索框（移动横向滚动 + 全宽）。
  - **Bento 网格（满足非全等）**：顶部一张 `wide`「监控概览」卡（按状态计数 mini 段）+ 各镜像 `1x1` 卡。
- **单卡信息层级**：
```
nginx:latest            [有更新]
● Docker
[已忽略说明条]（仅忽略时）
本地  sha256:ab12…cd34
远端  sha256:ef56…7890
最近检查：2026-09-04 10:00
[ 版本对比 ]            （整宽或 1/2）
[ 忽略/恢复 ]  [ 移除 ]  （gap-3）
```
- **交互（强制一致性规则 J）**：
  - 筛选 chips 单选高亮；`ignored` 本地过滤，其余按 `status` 请求。
  - 搜索 `reference` 不区分大小写；筛选/搜索变化回第 1 页。
  - 忽略/恢复：`setIgnored(id,!ignored)`→重载，文案切换。
  - **移除：点「移除」→ ConfirmDialog（§2.10），确认 `removeImage`→重载**；禁止页内直接危险按钮。
  - 新增：`addImage(reference)`；失败错误条。
- **状态**：loading→Spinner；空→BentoCard 内 EmptyState「没有匹配的镜像」；错误→顶部错误条。
- **[API]**：`images(status?)`→`[{id,reference,source,status,local_digest,remote_digest,last_check,ignored,tag}]`；`addImage`;`removeImage`;`setIgnored`。
- **移动**：§4.4；卡单列；筛选 chips 横向滚动。

### 5.4 版本对比 `/compare?id=`
- **目标**：单镜像本地 vs 远端、可用标签、版本时间线。
- **桌面布局**：左选择器（260px，可滚动，`reference`+StatusBadge，选中 `border-blue-500 ring-blue-500/20 bg-blue-50/50`）+ 右详情（`lg:h-[calc(100vh-230px)]` 内滚动）：
  1. 当前 vs 最新（wide 卡）：**内部两列用 `flex-1 divide-x divide-zinc-100 dark:divide-zinc-800`，各含图标+label+digest，无自身边框/阴影（禁止嵌套卡片）**。
  2. 可用标签（tall）：tag chips（`rounded-xl bg-zinc-100 ...`，最多 24，超出「+N」）。
  3. 版本时间线（整宽）：竖向时间线，节点圆点（最新 `amber`、历史 `zinc`）+ digest + 时间。
- **交互**：进入无 `id` 自动取首个 `setParams`；点击切换 `id`（URL 同步）；详情 loading→Spinner；不存在→EmptyState。
- **[API]**：`images()`→选择器；`image(id)`→`{image:{reference,tag,status,local_digest,remote_digest,last_check,last_update,source}, tags:[], versions:[{id,digest,scanned_at}]}`。
- **移动**：选择器改顶部横向滚动 chip 行；详情整宽在下方；时间线纵向滚。

### 5.5 更新通知 `/notifications`
- **目标**：集中查看提醒、管理已读。
- **布局**：页头（H1 + 「仅看未读」toggle + 「全部已读」primary[未读0禁用]）；概览 Bento（3 卡：总数/未读/更新可用引导）；通知**列表（`space-y-3`，每通知独立 BentoCard，非嵌套）**；分页(10)。
- **单卡**：`reference` + 类型徽标（有新版本 amber / 可选更新 blue）+ 未读圆点；`update` 类显 digest 变化条（旧→新），`new-tag` 类显 `当前 tag → 升级到 new_tag`；时间 + 「标记已读」。
- **交互**：「仅看未读」`notifications(true)` 重载；「标记已读」`markRead`；「全部已读」`markAllRead`（0 禁用）。
- **状态**：loading→Spinner；空→EmptyState「暂无通知」。
- **[API]**：`notifications(unread?)`→`[{id,reference,type,message,old_digest,new_digest,old_tag,new_tag,created_at,read}]`；`markRead`;`markAllRead`。
- **移动**：概览卡堆叠整行；行 `flex-wrap`；按钮满宽。

### 5.6 设置 `/settings`
- **目标**：配置运行参数与告警，保存即生效。
- **布局**：页头 + 表单 Bento：扫描间隔(wide,数字输入) / 演示列表(开关) / HTTP 注册表(开关) / 注册表镜像(wide,文本) / 钉钉(wide,Webhook+加签密钥+测试连接)。底部操作（保存 / 立即扫描）。
- **交互（强制一致性规则 K）**：开关仅写本地 `form`，「保存设置」才 `PUT /settings` 落地；钉钉「测试连接」`testDingTalk`→Toast，空 Webhook 禁用；保存 feedback 条 success/error。
- **状态**：加载→全页 Spinner；保存中按钮 loading。
- **[API]**：`settings()`→`{scan_interval,disable_default_watch,registry_insecure,registry_mirror,dingtalk_webhook,dingtalk_secret}`；`saveSettings`;`testDingTalk`;`scanNow`。
- **移动**：单列；开关卡「左文右开关」不换行；输入全宽；按钮满宽。

---

## 6. 强制一致性规则 + 风格自检清单

### 6.1 强制一致性规则汇总（A~K）
| 编号 | 规则 |
|---|---|
| A | 状态色板唯一（§1.1.4） |
| B | 字阶唯一（§1.2） |
| C | 间隙禁令：禁 gap-1/2/10/12，最小 gap-3（§1.3） |
| D | 圆角档位：卡 16 / 按钮输入徽章 12；禁 rounded-none/sm（§1.4） |
| E | Bento 栅格 4→2→1，须混合尺寸（§1.5） |
| F | 动效 ease-out，禁 bounce；hover 强表达版；reduced-motion（§1.6） |
| G | 禁止嵌套卡片；列表项用轻量行（§2.5） |
| H | 导航项固定 5 项（§3.2） |
| I | <lg 底部 Tab 栏（§4.1） |
| J | 危险操作二次确认（ConfirmDialog） |
| K | 设置改动显式保存 |

### 6.2 风格引用自检清单（交付前逐条确认）
- [ ] 按钮含 `px-4 py-2 md:px-6 md:py-3 rounded-xl font-medium transition-colors`
- [ ] 卡片含 `rounded-2xl border border-zinc-100 dark:border-zinc-800 shadow-sm ... transition-all duration-200`（hover 用强表达版）
- [ ] 输入框含 `bg-zinc-50 ... rounded-xl focus:ring-2 focus:ring-blue-500/20 focus:border-blue-500 transition-all`
- [ ] 无 `rounded-none` / `rounded-sm` / `gap-1` / `gap-2` / `gap-10` / `gap-12`
- [ ] 使用 CSS Grid `grid-cols-4`（桌面），有 `col-span-2`/`row-span-2`
- [ ] 卡片大小不一（非全等）
- [ ] 间隙一致（统一 gap-4/6）
- [ ] 圆角统一（xl/2xl）
- [ ] hover 上浮 + 微放大 + 阴影扩散
- [ ] 卡片内图标 `group-hover` 联动（变色/scale-110）
- [ ] 响应式在手机/平板/桌面稳定，无横向溢出
- [ ] 交互元素有焦点环 + 可访问名 + reduced-motion 方案
- [ ] 文本对比度 WCAG AA，状态非仅颜色
- [ ] 无嵌套卡片（卡片内不套卡片）
- [ ] 彩色背景上无灰色文字
- [ ] 无 bounce/elastic 缓动
- [ ] 无单侧粗边框装饰（border-left/right accent stripe）
- [ ] 无渐变文字（background-clip:text）
- [ ] 玻璃态未作为默认风格（仅登录屏轻玻璃）
- [ ] 未在每个 section 标题上放 tiny uppercase eyebrow
- [ ] 移动端：<lg 底部 Tab 栏；内容 `pb-24`；触控 ≥44px

---

## 7. 工程师落地指引

### 7.1 文件结构
```
frontend/src/
  styles/tokens.css        # §1 变量 + 基础类（沿用 index.css，补充变量与 gap 禁令）
  components/  Button Input Switch Badge(StatusBadge) Card(BentoCard) StatCard
               Pagination Spinner EmptyState ConfirmDialog Toast SourceTag BottomTabBar
  context/  ThemeContext AuthContext (保留)
  pages/  (6 页按 §5 重构)
  api/client.js (保留)
```

### 7.2 `tailwind.config.js` 补充
- `darkMode:'class'`（保留）；`fontFamily.sans/mono`（保留 Plus Jakarta Sans / JetBrains Mono）。
- 建议新增 `boxShadow.bento:'0 8px 30px rgba(0,0,0,0.08)'`；`borderRadius` 已有 xl/2xl。
- **间隙治理**：CI/ESLint 加规则拦截 `gap-1 gap-2 gap-10 gap-12`（或约定 code review 检查）。
- 优先用 zinc/emerald/amber/blue/rose 刻度 + `dark:`，勿散用自定义色。

### 7.3 实施顺序
1. tokens.css + Tailwind 补充 → 2. 组件库（含 BottomTabBar / ConfirmDialog / Toast）→ 3. Layout（桌面侧栏 + 移动底 Tab + 顶条）→ 4. 逐页重构（Login→Dashboard→Images→Compare→Notifications→Settings，注意 Compare 去嵌套、Images 加概览宽卡）→ 5. 暗色 + 移动回归 → 6. 键盘/读屏走查 + §6.2 清单。

### 7.4 易错点
- 移除务必 ConfirmDialog（规则 J）；Compare 内部两列**不要**再包 `rounded-2xl+shadow` 子卡（规则 G）。
- 状态色查 §1.1.4 表，勿把 `update-available` 写成 red。
- digest 统一 `shortDigest()` + `title` 完整值。
- 间隙最小 `gap-3`，遇到 `gap-2` 立即改 `gap-3`（现有代码含 `gap-2` 需迁移）。
- 彩色 Hero 上只用白字，禁灰字。

---

## 8. 验收清单（交付前自检）
- [ ] 6 页统一页头模式（H1+副标题 / 主行动）。
- [ ] 所有状态色来自 §1.1.4，无散色；彩色底无灰字。
- [ ] 暗色每处浅色均有 `dark:` 映射（§0.2.5）。
- [ ] 移除走 ConfirmDialog；忽略可逆不确认。
- [ ] 加载有 Spinner/骨架，无白屏。
- [ ] 成功/失败走 Toast 或提示条。
- [ ] 键盘可主流程；焦点环可见。
- [ ] 移动端 <lg 底部 Tab 栏 + `pb-24` + 触控 ≥44px；无横向溢出。
- [ ] Bento 网格 4→2→1，混合尺寸，无全等卡。
- [ ] `prefers-reduced-motion` 动效降级。

---
*文档结束 ｜ 本规范为 DockMon 前端视觉与交互的唯一事实来源（single source of truth），融合 Bento Grid 官方风格引用 v1.1，后续迭代须据此评审。*
