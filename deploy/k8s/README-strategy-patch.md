# Blog_v3 k8s 部署策略补丁（2026-11-01）
# 背景：bbolt(agent.db) 是独占 flock，RollingUpdate 默认 maxSurge=1 会导致新旧 pod
# 重叠期间新 pod 抢锁失败，agent 路由不注册（详见 internal/agent/store.go 注释）。
# 应用：kubectl patch deployment blog-front -n apps --type=strategic -p "$(cat 本文件去掉注释)" 
# 或：kubectl apply -f 本文件 不适用（这是 patch 片段），用下方命令。
# 命令：kubectl patch deployment blog-front -n apps --type=strategic -p \
#   '{"spec":{"strategy":{"type":"RollingUpdate","rollingUpdate":{"maxSurge":0,"maxUnavailable":1}}}}'
