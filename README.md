# Ackermann 转向几何核算服务

一个**可独立部署的纯服务端**组件：上游提交轴距 \(L\)、轮距 \(T\) 与内侧前轮转角，
服务返回外侧转角、转向瞬心（ICR）与转弯半径 \(R\)、四轮转弯半径（含瞬心平面坐标）、
内外轮滚过弧长比。低速、无侧偏假设。不含派单、车型商城、前端页面或账户体系。

- 语言：Go（标准库 `net/http`，仅持久化驱动一个三方依赖 `pgx`）
- 持久化：PostgreSQL（`docker compose` 一键起）；未配置数据库时自动降级为内存存储
- 角度单位：服务内部统一为**度**；请求可用 `angle_unit: "deg" | "rad"` 声明

## 几何约定

坐标：以后轴中点为原点，\(x\) 指向车头、\(y\) 指向车辆左侧。正转角 = 向左打。

- \(R\)：后轴中点到瞬心的**有符号**距离（瞬心位于后轴延长线上）。
- 等效自行车模型：\(\tan\delta = L/R\)。
- 阿克曼条件（内侧 \(d_i\)、外侧 \(d_o\)，取绝对值）：

  \[
  \cot d_o - \cot d_i = \frac{T}{L}
  \]

- 记后内轮到瞬心的距离 \(r_i = L/\tan|d_i|\)，则：
  - \(R = r_i + T/2\)（后轴中点基准）
  - 后内轮半径 \(= r_i\)，后外轮半径 \(= r_i + T\)（即 \(R \mp T/2\)）
  - 前内轮半径 \(= \sqrt{L^2 + r_i^2}\)，前外轮半径 \(= \sqrt{L^2 + (r_i+T)^2}\)
- 弧长比：四轮绕同一瞬心、角速度相同，\(s \propto r\)，故弧长比等于半径比。
- **直行（内侧转角为 0）**：外侧转角与自行车转角为 0，瞬心在无穷远，
  所有半径字段返回 JSON `null`，绝不伪造有限半径。
- 左打/右打只差符号：所有有符号半径、转角取反，绝对值一致，瞬心关于 \(x\) 轴对称。

### 几何自检（不是只核对两个角度）

服务对每次结果做几何检查：分别取前内轮、前外轮航向的垂线（即到瞬心的半径方向），
求交得到候选瞬心，再检查该点是否同时位于四个车轮各自的垂线上，且与解析中心
\((0,R)\) 重合。响应中给出 `geometry_ok` 与最大偏差 `geometry_max_deviation`（米），
容差为 `abs_tol + rel_tol * (L + |R|)`，可在 `/config` 查询。

### 预置算例

`GET /api/v1/steering/preset`：\(L=2.7\text{m},\ T=1.55\text{m},\ d_i=30^\circ\)

- 外侧转角 ≈ **23.44°**（明显小于 30°）
- \(R ≈ 5.452\text{m}\)，自行车角 ≈ 26.35°
- 后内/后外半径 ≈ 4.677 / 6.227 m，后外/后内弧长比 ≈ 1.331

## HTTP 接口

基址 `/api/v1/steering`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/calc` | 单次转向核算（外侧角、R、自行车角、弧长比、四轮结果） |
| POST | `/wheels` | 四轮半径接口（四轮半径/转角、瞬心坐标、弧长比） |
| POST | `/batch` | 批量核算：`{"items":[ {...}, {...} ]}`，逐项成功/失败 |
| GET  | `/history` | 历史查询：成功与失败请求都持久化 |
| GET  | `/config` | 回显角度单位与几何容差配置 |
| GET  | `/preset` | 预置算例 |
| GET  | `/healthz` | 存活探针（监控采集） |
| GET  | `/readyz` | 就绪探针（探测存储连通性） |

请求体示例：

```json
{ "wheelbase": 2.7, "track": 1.55, "inside_angle": 30 }
```

可选 `"angle_unit": "rad"`，此时角度按弧度入参（服务仍以度出参）。

历史查询参数：`success=true|false`、`kind=single|batch_item`、`direction=left|right|straight`、
`batch_id=...`、`since`/`until`（RFC3339）、`limit`（≤500，默认50）、`offset`。

### 错误响应（可读、字段定位、不会 500 崩溃）

```json
{
  "error": {
    "code": "angle_out_of_range",
    "message": "item 1: inside angle magnitude must be strictly < 90 degrees, got 90",
    "details": { "field": "inside_angle", "reason": "angle_out_of_range" }
  }
}
```

批量中某一项非法时，HTTP 仍为 200，错误挂在该项上并给出 **1-based 的 item 序号**
（`details.item_index` 为 0-based）与出问题的字段，其余各项照常出结果。

拒绝条件：缺字段、非数值、非有限值（NaN/Inf）、轴距或轮距 ≤ 0、
内侧转角绝对值 ≥ 90°、角度单位无法识别、数值与声明的角度单位互相矛盾
（如声称弧度却给 10、声称度却给 400）。

## 运行

### Docker Compose（服务 + PostgreSQL）

```bash
docker compose up -d --build
curl -s localhost:8080/api/v1/steering/preset | python3 -m json.tool
```

### 本地直接运行（无数据库，内存存储）

```bash
go run ./cmd/ackermann
# 或
ADDR=:8080 DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable ./ackermann
```

## 测试

```bash
make test          # 全部单元/HTTP/存储测试
make test-race     # 带竞态检测（含并发互不串扰用例）
docker compose up -d db && make test-postgres   # PostgreSQL 集成测试
```

覆盖的规则：余切差等于 \(T/L\)、内侧为零则外侧为零且半径为 null、非零时内侧角严格
大于外侧角、轴距加倍半径近似加倍（后内轮半径精确加倍）、轮距加倍内外差变大、左右打
只差符号、四轮半径几何上交于一点、超 90° 拒绝、非法参数拒绝、批量部分失败其余成功、
历史（含失败原因）持久化、80 路并发请求结果互不串扰。

## 代码结构

```
ackermann/           # 纯几何：compute.go 转角关系/半径，verify.go 瞬心几何校验，
                     # validate.go 输入校验，units.go 单位与预置算例，types.go
storage/             # store.go 接口，memory.go 内存实现（带锁），postgres.go PG 实现
api/                 # handlers.go 路由与接口，parse.go 请求解析/错误，middleware.go
cmd/ackermann/       # main：装配存储与 HTTP 服务
Dockerfile docker-compose.yml Makefile
```
