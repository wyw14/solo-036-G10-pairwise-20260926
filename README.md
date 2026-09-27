PigFlow is a local HTTP control service for long-distance pipeline pigging operations.

## 线路配置版本（line versions）

线路拓扑（站点、区段、压力限制）不再由硬编码默认值决定，而是以带版本号、可审核、可回看的配置版本管理。系统首次启动会内置一份已发布的基线版本 `v1`。

版本生命周期：`draft`（草拟，可反复录入与校验）→ `published`（发布后内容冻结，新作业一律采用新版本；旧作业永久保留其创建时的版本号，可回看当时的线路）。

- `GET  /api/line-versions`                列出全部版本（含草拟）
- `GET  /api/line-versions/current`        查看当前已发布版本
- `POST /api/line-versions`                新建草拟（默认复制当前版本内容），body：`{"note":"..."}`
- `GET  /api/line-versions/{n}`            按版本号回看
- `PUT  /api/line-versions/{n}`            录入/覆盖草拟的站点、区段、压力限制
- `POST /api/line-versions/{n}/validate`   校验草拟（不发布），非法时返回 422 与具体原因
- `POST /api/line-versions/{n}/publish`    校验并发布；存在在途作业时返回 409
- `GET  /api/operations/{runID}/line-version` 查看某作业当时采用的线路版本

`PUT` 请求体示例：

```json
{
  "note": "收紧 a-b 段压力限制",
  "stations": [
    {"id": "station-a", "name": "North Launcher", "role": "launcher", "kilometer": 0, "pressure_limit": 12.5, "enabled": true},
    {"id": "station-b", "name": "Central Block Valve", "role": "intermediate", "kilometer": 86.4, "pressure_limit": 11.8, "enabled": true},
    {"id": "station-c", "name": "South Receiver", "role": "receiver", "kilometer": 173.2, "pressure_limit": 11.2, "enabled": true}
  ],
  "segments": [
    {"id": "segment-a-b", "from": "station-a", "to": "station-b", "length": 86.4, "maximum_pressure": 11.5, "enabled": true},
    {"id": "segment-b-c", "from": "station-b", "to": "station-c", "length": 86.8, "maximum_pressure": 11.2, "enabled": true}
  ]
}
```

校验规则至少包括：站点/区段标识完整且不重复、区段端点存在且里程顺流向下、区段压力上限不得超过相邻站点压力上限、全线至少包含一个发球端与一个收球端。每个已发布版本携带规范化的内容 SHA-256 摘要（`content_checksum`）用于审核；版本记录随控制快照一并持久化，恢复后旧作业与版本历史均可继续查询。
