# CLI 开发

Go 1.25；运行 `go test ./...`。`internal/relay` 负责宿主控制与来源核对，`internal/app/session*` 负责接续生命周期和交互，`internal/localstate` 是本机状态 writer，`internal/distribution` 负责独立安装资源与宿主权限计划。

协议来自 [OpenAPI](../contracts/openapi.json)。后端 schema 更新后，在 cli 目录运行 `python3 tools/generate.py`；`--check` 比较确定生成结果。生成工具固定 oapi-codegen 版本并删除非机器语义注释。

真实宿主边界与飞书送达须另外验证。测试及运行证据放仓库外，不能将替身、接口可用或退出 0 冒充实际接续。

安装态贯穿测试在完整安装后运行：`安装目录/.venv/bin/python cli/tools/relay_integration.py --install-root 安装目录`。脚本使用安装 CLI、API、迁移及独立临时 PostgreSQL，宿主和飞书边界使用仓库 fixture。覆盖去重、来源、问题续答、双端断开恢复、二进制替换、历史故障及摘要慢调用、回执丢失和桥接重启；不会连接真实飞书或修改真实宿主 profile。数据库、日志和一次性状态均留在仓库外，结束后停止隔离服务。
