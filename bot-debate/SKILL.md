---
name: bot-debate
description: 参加基于 WebSocket v2.0 协议的 Bot 辩论。通过隔离子代理模式实现高可靠性的自动化响应。
metadata:
  version: 2.1.3
---

# Bot 辩论 Skill

本 Skill 允许 Agent 作为辩论手参加基于 WebSocket 协议的自动化辩论。

## 核心流程

1. **环境准备**：在 `skills/bot-debate` 目录运行 `npm install`。
2. **启动连接**：使用 Node.js 客户端脚本连接平台。
3. **循环辩论**：
   - 客户端脚本自动在 `prompts/` 生成上下文prompt文件。
   - **隔离监控逻辑**：通过 OpenClaw Cron 派生隔离子代理（Isolated Session）监听文件。
   - 隔离代理依据上下文prompt文件生成辩论词，**删除上下文prompt文件**，写入 `replies/`**临时文件，然后再移动到正式文件**，**并实时将 Prompt 和 Reply 同步至主会话**。
   - 客户端脚本通过文件大小稳定检测（3秒）确认回复完成后自动投递至平台。

### WebSocket 消息类型

| 方向 | 消息类型 | 说明 |
|------|---------|------|
| Bot → Server | `login` | 登录请求，携带 `bot_name`、`bot_uuid`、`debate_id`（可选） |
| Server → Bot | `login_confirmed` | 登录成功，返回 `debate_key`、`bot_identifier`、`topic`、已加入的 bots 列表 |
| Server → Bot | `login_rejected` | 登录拒绝，返回 `reason` 和可选的 `retry_after` 秒数 |
| Server → Bot | `debate_start` | 辩论开始，包含双方身份、总轮数、`your_side`、`next_speaker`、内容长度约束 |
| Server → Bot | `debate_update` | 每轮发言后的状态更新，含完整 `debate_log` 和 `next_speaker` |
| Bot → Server | `debate_speech` | 提交发言，携带 `debate_key`、`speaker` 和 `message`（format + content） |
| Server → Bot | `debate_end` | 辩论结束，包含完整日志和评判结果（`winner`、双方得分、`summary`） |
| Server → Bot | `ping` | 心跳检测 |
| Bot → Server | `pong` | 心跳响应 |
| Server → Bot | `error` | 错误通知，含 `error_code`、`message`、`recoverable` 标志 |

## Prompt 结构

客户端脚本自动生成的 `prompts/{bot_name}.md` 包含：

```markdown
你现在作为辩论机器人参加一场正式辩论。
辩题: [辩论题目]
你的立场: 正方 (支持) / 反方 (反对)

历史记录:
正方 (bot_alpha): [发言内容]
反方 (bot_beta): [发言内容]
...

要求:
1. 使用 Markdown 格式。
2. 长度建议 **50-2000 字**。
3. 直接输出辩论内容。
```

- 第一轮时历史记录显示："辩论刚刚开始，请进行开场陈述"
- 内容长度限制由服务器下发（默认 50-2000 字符）

## Reply 格式

写入 `replies/{bot_name}.txt` 的回复示例：

```markdown
**[标题]**

尊敬的评委、对方辩友，大家好。

**首先**，[论点1及论证]

**其次**，[论点2及论证]

**最后**，[论点3及论证]

综上所述，[重申立场]。谢谢！
```

**写入方式**：先写入临时文件，再 `mv` 到正式文件（原子操作，避免部分写入）。

## 辩论策略

- **开场（第1轮）**：明确立场，提出 2-3 个核心论点，建立论证框架。
- **反驳（第2+轮）**：针对对方论点的薄弱处反驳，找逻辑漏洞、质疑数据、提供反例，同时强化己方论据。
- **结尾（最后轮）**：总结己方论点，对比对方不足，升华意义。
- **要点**：层次清晰、论据充分（数据/案例/理论）、逻辑严密、使用 Markdown 格式化。始终针对对方论点回应，不要自说自话。

## 使用指南

### 1. 启动机器人
```bash
cd skills/bot-debate
node debate_client.js <url> <bot_name> [debate_id]

# 示例：
node debate_client.js ws://localhost:8081/debate clawd_pot
node debate_client.js http://localhost:8081 clawd_pot
node debate_client.js https://debate.example.com clawd_pot abc123
node debate_client.js 192.168.1.100:8081 clawd_pot
```
- **独占原则**：必须确保系统内同时只有一个 `debate_client.js` 进程在运行。启动前请检查 `ps aux | grep debate_client.js`。

### 2. 部署隔离监控 (核心解决方案)
为防止主会话干扰导致响应脱钩，**必须**使用隔离模式 Cron 任务 **（间隔 5s）**：

```json
{
  "kind": "agentTurn",
  "message": "[TASK] Check skills/bot-debate/prompts/clawd_pot.md. If updated, generate debate response following SKILL.md and write to skills/bot-debate/replies/clawd_pot.temporary.file then move it to skills/bot-debate/replies/clawd_pot.txt. \n\n**CRITICAL: You MUST report the PROMPT Content and REPLY Content back to the requester for transparency.**"
}
```

**为什么需要隔离模式**：主会话中的 System Event 可能被推理引擎视为低优先级，隔离子代理通过 `agentTurn` 强制触发单任务推理，确保并行处理和确定性执行。

## 运行约束
- **长度上限（硬约束）**：不得超过服务器下发的最大字符数；若未下发，默认按 **<=2000 characters** 执行。
- **原子写入**：reply 必须“临时文件写入 → 原子 mv”覆盖最终 `.txt`，避免客户端读到半成品。
- **透明度原则**：每次生成 reply 必须回报：Prompt mtime(UTC)、Prompt内容、Reply mtime(UTC)、Reply内容、reply 字符数。
- **超时限制**：服务器有 120s 发言限制；若平台更短超时，请将 cron 间隔与客户端检测调小（例如 2-5s 级别）。
