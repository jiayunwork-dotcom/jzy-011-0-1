# Ackermann 转向几何核算服务

一个可独立部署的纯服务端车辆低速转向几何计算组件。上游提交**轴距、轮距、内侧车轮转角**，
服务返回外侧转角、等效自行车模型转角、后轴中点到瞬心的转弯半径 R、瞬心平面坐标、
四轮转弯半径，以及外轮/内轮滚过的弧长比。

本服务不包含派单、车型商城、前端页面或账户体系。

---

## 1. 几何模型与约定

低速、无侧偏，所有车轮速度方向垂直于各自到瞬心（ICR）的半径，四条半径交于同一点。

* 坐标原点 O 为**后轴中点**；+X 指向车头，+Y 指向驾驶员左侧。
* `R` = 后轴中点 O 到瞬心的距离。
* 转角在服务内部统一为**度**（接口可选 `rad`，见下）。
* 左打为正、右打为负；左右打只有符号差别，所有半径与角度绝对值相同。

核心恒等式（L=轴距，T=轮距，u=|内角|，v=|外角|）：

```
cot(v) - cot(u) = T / L          # 余切差
tan(bike)       = L / R          # 等效自行车模型
R               = L·cot(u) + T/2
v               = atan(L / (R + T/2))
```

四轮半径（后轮以后轴中点为基准）：

```
后内轮半径 = R - T/2        后外轮半径 = R + T/2
前内轮半径 = sqrt((R - T/2)^2 + L^2)
前外轮半径 = sqrt((R + T/2)^2 + L^2)
弧长比(外/内) = 对应半径之比
```

硬性几何约束：

* L、T 必须严格为正；
* |内侧转角| 必须**严格小于 90°**；
* 非零转角时 |内角| > |外角|；
* 内侧转角为 0 时，外侧转角必须为 0，瞬心在无穷远——此时**不会**返回任何有限假半径，
  `turn_radius_m`、`instant_center_xy_m`、`radii`、弧长比均为 `null`。

四轮半径是否共点不是只比两个转角数字，而是对每个车轮做几何检查：
`residual = (ICR - 车轮接地点) · 行驶方向单位向量`，四轮的残差必须都为 0
（容差 `icr_tolerance_mm`，默认 1e-6 mm）。响应里每个车轮都带 `icr_residual_mm`。

预置算例：L=2.70 m，T=1.60 m，内角 30° → **外角 ≈ 23.276°**（明显小于 30°），
R ≈ 5.477 m，后内/后外 ≈ 4.677 / 6.277 m。

---

## 2. 快速开始

### 2.1 Docker Compose（服务 + PostgreSQL，一键启动）

```bash
docker compose up --build
# 服务: http://localhost:8080   PostgreSQL: localhost:5432
```

健康检查：

```bash
curl localhost:8080/healthz    # 进程存活
curl localhost:8080/readyz     # 依赖（数据库）就绪
```

### 2.2 本地直接运行（默认内存存储，无需数据库）

```bash
go run ./cmd/server -addr=:8080
# 或显式: STORAGE=memory go run ./cmd/server
```

接 PostgreSQL：

```bash
STORAGE=postgres \
DATABASE_DSN='postgres://ackermann:ackermann@localhost:5432/ackermann?sslmode=disable' \
go run ./cmd/server
```

启动时会自动建表（`steering_records`），并对数据库未就绪做退避重试。

### 2.3 运行测试

```bash
go test ./...                      # 全部单元/HTTP 测试
go test -race ./...                # 含竞态检测（并发互不串扰）
go test -v ./internal/geometry     # 只看几何规则测试
```

PostgreSQL 后端集成测试默认跳过；当本机/容器已有数据库时：

```bash
ACKERMANN_TEST_DSN='postgres://ackermann:ackermann@localhost:5432/ackermann?sslmode=disable' \
  go test ./internal/store -run TestPostgresBackend -v
```

---

## 3. HTTP 接口

所有请求/响应均为 JSON。角度默认单位为度，可在单条请求里加 `"angle_unit":"rad"`
（服务内部恒为度，响应恒为度）。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/v1/steering/calculate` | 单次转向核算 |
| `POST` | `/api/v1/steering/radii` | 单次核算，额外返回四轮布局与逐轮几何残差 |
| `POST` | `/api/v1/steering/batch` | 批量核算，单项失败不影响其他项 |
| `GET`  | `/api/v1/history` | 按条件查询历史 |
| `GET`  | `/api/v1/history/{id}` | 取单条历史 |
| `GET`  | `/api/v1/steering/preset` | 预置可直接调用算例 |
| `GET`  | `/api/v1/config` | 回显角度单位与几何容差 |
| `GET`  | `/healthz` / `/readyz` | 存活 / 就绪探针 |

### 3.1 单次核算

请求：

```json
{ "wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30 }
```

响应：

```json
{
  "ok": true,
  "record_id": "rec_...",
  "result": {
    "wheelbase_m": 2.7,
    "track_m": 1.6,
    "inner_angle_deg": 30,
    "outer_angle_deg": 23.276094121345878,
    "bicycle_angle_deg": 26.24386260348082,
    "turn_direction": "left",
    "turn_radius_m": 5.476537180435969,
    "instant_center_xy_m": [0, 5.476537180435969],
    "radii": {
      "rear_inner_m": 4.676537180435969,
      "rear_outer_m": 6.276537180435969,
      "front_inner_m": 5.4,
      "front_outer_m": 6.832636312390342
    },
    "arc_length_ratio_outer_over_inner": {
      "rear_outer_over_inner": 1.3421334928531115,
      "front_outer_over_inner": 1.2653030208130263
    },
    "max_icr_residual_mm": 2.6e-13,
    "straight": false
  }
}
```

`/radii` 的结果额外包含 `wheels` 数组（四个车轮的坐标、有符号/绝对半径、转角、残差），
用于调用方核对四条半径共点。

直行（内角 0）：

```json
{ "wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 0 }
```

```json
{
  "ok": true,
  "result": {
    "inner_angle_deg": 0, "outer_angle_deg": 0, "bicycle_angle_deg": 0,
    "turn_direction": "straight",
    "turn_radius_m": null,
    "instant_center_xy_m": null,
    "radii": null,
    "arc_length_ratio_outer_over_inner": null,
    "straight": true
  }
}
```

### 3.2 批量核算

```json
{ "items": [
  { "wheelbase_m": 2.7, "track_m": 1.6, "inner_angle": 30 },
  { "wheelbase_m": 0,   "track_m": 1.6, "inner_angle": 30 },
  { "wheelbase_m": 2.5, "track_m": 1.5, "inner_angle": -15 }
]}
```

单项非法不会中断整批；响应逐项给出 `index`、`ok`，失败项的 `error.fields`
会指出**第几组的哪个参数**有问题（示例中第 2 组的 `wheelbase_m`）。
批量本身（含每个子项）都会持久化，子项通过 `batch_id` 关联。

### 3.3 历史查询

```
GET /api/v1/history?status=ok&kind=single&source=calculate&direction=left
                    &batch_id=bat_...&min_radius_m=4&max_radius_m=10
                    &since=2026-09-01T00:00:00Z&until=2026-09-19
                    &limit=50&offset=0
```

所有过滤参数都可选；返回 `{total, count, items[]}`，按时间倒序。
每条记录包含原始请求、返回结果/错误、状态、方向、半径、来源和时间戳。

### 3.4 配置回显

```bash
curl localhost:8080/api/v1/config
# {"angle_unit":"deg","output_angle_unit":"deg","icr_tolerance_mm":0.000001,"numeric_eps_deg":1e-9}
```

---

## 4. 错误处理

错误**不会**算出看似正常的几何，也不会让进程崩溃；非法请求返回 4xx
（几何不可行返回 422），结构固定为：

```json
{
  "ok": false,
  "error": {
    "code": "invalid_parameters",
    "message": "one or more parameters are invalid",
    "fields": [
      { "field": "wheelbase_m", "message": "must be strictly positive, got 0" }
    ]
  }
}
```

覆盖：缺字段、非数值（字符串/布尔/数组/`null`）、非有限值、轴距或轮距非正、
|内角| ≥ 90°、未知/非字符串角度单位、单位与数值互相矛盾（如 30 标成 `rad`
会换算成约 1719°，直接拒绝）、请求体非法 JSON、未知字段、空请求体等。
批量中会额外标注出错项的 `index` 与字段名。

---

## 5. 持久化

* `STORAGE=memory`（默认）：带锁内存实现，零外部依赖；重启清空，适合开发/演示。
* `STORAGE=postgres`：PostgreSQL（pgx 连接池），自动建表与索引；生产使用。

两种后端实现同一接口（`internal/store`），HTTP 与并发行为一致。
每次核算（成功或失败）都保存原始请求与返回内容；批量保存一条汇总 + 每子项一条。

---

## 6. 代码结构

```
cmd/server/main.go              启动、配置、信号优雅退出、存储选择与重试
internal/
  geometry/
    geometry.go                 内角->外角/R/自行车角 的转角关系（纯函数）
    radii.go                    四轮半径、瞬心坐标、四轮共点几何残差检查
    result_helpers.go
  validate/validate.go          请求解析与逐字段输入/单位校验
  store/
    store.go                    存储接口、记录/过滤模型
    memory.go                   并发安全的内存实现
    postgres.go                 PostgreSQL 实现（建表、索引、过滤查询）
    id.go
  service/
    service.go                  DTO、服务配置
    calc.go                     单次核算编排（校验→几何→容差→持久化）
    batch.go                    批量核算（逐项隔离、部分失败、汇总）
    history.go                  历史查询与查询参数解析
    preset.go                   预置算例
  api/server.go                 HTTP 路由、编解码、错误信封、panic 兜底
Dockerfile                      多阶段构建（debian:bookworm-slim 运行时）
docker-compose.yml              app + postgres(16) 一键启动
.env.example
```

## 7. 自动化测试覆盖

* 余切差 `cot(外)-cot(内) = T/L`
* 内角为 0 则外角为 0、无有限假半径
* 非零时 |内角| > |外角|
* 轴距加倍则 `L·cot(u)=R-T/2` 精确加倍、R 近似加倍、外角更靠近内角
* 轮距加倍则内外角之差变大
* 左右打方向只差符号，角度/有符号半径全部取反、绝对值不变
* 四轮半径在平面上交于一点（独立求两前轮法线与后轴交点 + 逐轮距离/残差，几何核对）
* |内角| ≥ 90° 被拒；非法参数（缺字段/非数值/非正/单位矛盾等）被拒
* 批量部分失败：指出第几组哪个参数，其余组正常返回
* 历史持久化正确（成功与失败都落库、过滤可用、批量关联）
* 并发多请求结果互不串扰、记录不丢失（`go test -race`）
