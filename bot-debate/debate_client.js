const WebSocket = require('ws');
const { v4: uuidv4 } = require('uuid');
const fs = require('fs');
const path = require('path');

class DebateClient {
    constructor(wsUrl, botName, debateId = null) {
        this.botName = botName;
        this.botUuid = uuidv4();
        this.wsUrl = this.convertToWebSocketUrl(wsUrl);
        this.debateId = debateId;
        this.debateKey = null;
        this.botIdentifier = null;
        this.ws = null;
        this.minContentLength = 50;    // Default values
        this.maxContentLength = 2000;  // Default values

        // Ensure directories exist
        if (!fs.existsSync('prompts')) fs.mkdirSync('prompts');
        if (!fs.existsSync('replies')) fs.mkdirSync('replies');
    }

    convertToWebSocketUrl(url) {
        let wsUrl;

        // If already a WebSocket URL, use as is
        if (url.startsWith('ws://') || url.startsWith('wss://')) {
            return url;
        }

        // Convert HTTP/HTTPS to WS/WSS
        if (url.startsWith('https://')) {
            wsUrl = url.replace('https://', 'wss://');
        }
        else if (url.startsWith('http://')) {
            wsUrl = url.replace('http://', 'ws://');
        }
        // If no protocol, assume ws://
        else {
            wsUrl = 'ws://' + url;
        }

        // Add /debate path if not already present
        if (!wsUrl.includes('/debate')) {
            wsUrl = wsUrl.replace(/\/$/, '') + '/debate';
        }

        return wsUrl;
    }

    log(msg) {
        console.log(`[${new Date().toISOString()}] [${this.botName}] ${msg}`);
    }

    send(type, data) {
        const payload = JSON.stringify({
            type: type,
            timestamp: new Date().toISOString(),
            data: data
        });
        this.ws.send(payload);
    }

    async handleTurn(msgData) {
        this.log("My turn to speak.");

        // Update content length constraints from server
        if (msgData.min_content_length !== undefined) {
            this.minContentLength = msgData.min_content_length;
        }
        if (msgData.max_content_length !== undefined) {
            this.maxContentLength = msgData.max_content_length;
        }

        let history = "";
        if (msgData.debate_log) {
            msgData.debate_log.forEach(entry => {
                const side = entry.side === 'supporting' ? '正方' : '反方';
                history += `\n${side} (${entry.speaker}): ${entry.message.content}\n`;
            });
        }

        const prompt = `
你现在作为辩论机器人参加一场正式辩论。
辩题: ${msgData.topic}
你的立场: ${msgData.your_side === 'supporting' ? '正方 (支持)' : '反方 (反对)'}

历史记录:
${history || "辩论刚刚开始，请进行开场陈述。"}

要求:
1. 使用 Markdown 格式。
2. 长度要求: 最少 ${this.minContentLength} 字符，最多 ${this.maxContentLength} 字符。
3. 直接输出辩论内容。
`;

        // 使用临时文件 + rename 原子操作
        const tempFile = `prompts/.${this.botName}.${Date.now()}.tmp`;
        fs.writeFileSync(tempFile, prompt);
        fs.renameSync(tempFile, `prompts/${this.botName}.md`);
        this.log(`Prompt saved. Waiting for replies/${this.botName}.txt...`);

        const replyPath = `replies/${this.botName}.txt`;

        // 清空回复文件
        if (fs.existsSync(replyPath)) fs.writeFileSync(replyPath, "");

        let lastSize = -1;       // 记录上次文件大小
        let stableCount = 0;     // 文件大小稳定计数器

        const checkReply = () => {
            if (!fs.existsSync(replyPath)) {
                return false;
            }

            // 获取文件大小
            const fileSize = fs.statSync(replyPath).size;

            // 文件为空，跳过
            if (fileSize === 0) {
                return false;
            }

            // 文件大小稳定性检测
            if (fileSize === lastSize) {
                stableCount++;
            } else {
                stableCount = 0;
                lastSize = fileSize;
            }

            // 需要连续 3 次检查（3秒）大小不变
            if (stableCount < 3) {
                return false;
            }

            // 读取内容并验证长度
            const content = fs.readFileSync(replyPath, 'utf8').trim();
            const contentLen = content.length;

            if (contentLen < this.minContentLength) {
                this.log(`Warning: Content too short (${contentLen} chars, minimum ${this.minContentLength}). Please provide more content.`);
                stableCount = 0;  // 重置计数
                return false;
            }

            if (contentLen > this.maxContentLength) {
                this.log(`Warning: Content too long (${contentLen} chars, maximum ${this.maxContentLength}). Please shorten your response.`);
                stableCount = 0;  // 重置计数
                return false;
            }

            // 发送消息
            this.send('debate_speech', {
                debate_id: this.debateId,
                debate_key: this.debateKey,
                speaker: this.botIdentifier,
                message: { format: 'markdown', content: content }
            });

            // 清空文件
            fs.writeFileSync(replyPath, "");
            this.log(`Speech submitted (${contentLen} chars).`);
            return true;
        };

        const interval = setInterval(() => {
            if (checkReply()) clearInterval(interval);
        }, 1000);
    }

    run() {
        this.ws = new WebSocket(this.wsUrl);

        this.ws.on('open', () => {
            if (this.debateId) {
                this.log("Connected. Logging in...");
                this.send('bot_login', {
                    bot_name: this.botName,
                    bot_uuid: this.botUuid,
                    debate_id: this.debateId,
                    version: "2.0"
                });
            } else {
                this.log("Connected. Requesting debate assignment...");
                this.send('bot_login', {
                    bot_name: this.botName,
                    bot_uuid: this.botUuid,
                    version: "2.0"
                });
            }
        });

        this.ws.on('message', (data) => {
            const msg = JSON.parse(data);
            const { type, data: msgData } = msg;

            switch (type) {
                case 'login_confirmed':
                    this.debateKey = msgData.debate_key;
                    this.botIdentifier = msgData.bot_identifier;
                    if (msgData.debate_id && !this.debateId) {
                        this.debateId = msgData.debate_id;
                        this.log(`Assigned to debate: ${this.debateId}`);
                    }
                    this.log(`Login confirmed as ${this.botIdentifier}`);
                    this.log(`Topic: ${msgData.topic}`);
                    if (msgData.joined_bots && msgData.joined_bots.length > 0) {
                        this.log(`Already joined bots: ${msgData.joined_bots.join(', ')}`);
                    } else {
                        this.log(`You are the first bot to join`);
                    }
                    break;
                case 'login_rejected':
                    this.log(`Login rejected: ${msgData.message}`);
                    this.log(`Reason: ${msgData.reason}`);
                    if (msgData.retry_after) {
                        this.log(`You can retry after ${msgData.retry_after} seconds`);
                    }
                    process.exit(1);
                    break;
                case 'debate_start':
                case 'debate_update':
                    if (msgData.next_speaker === this.botIdentifier) {
                        this.handleTurn(msgData);
                    }
                    break;
                case 'debate_end':
                    this.log(`Debate ended. Winner: ${msgData.debate_result.winner}`);
                    this.ws.close();
                    break;
                case 'ping':
                    // Server sent ping, respond with pong
                    this.send('pong', {
                        client_time: new Date().toISOString()
                    });
                    break;
                case 'error':
                    this.log(`Error: ${msgData.message}`);
                    break;
                default:
                    this.log(`Unknown message type: ${type}`);
            }
        });

        this.ws.on('error', (error) => {
            this.log(`WebSocket error: ${error.message}`);
        });

        this.ws.on('close', (code, reason) => {
            this.log(`Connection closed (code: ${code}, reason: ${reason || 'no reason'})`);
            process.exit(0);
        });
    }
}

const [,, url, botName, debateId] = process.argv;

if (!url || !botName) {
    console.log("Usage: node debate_client.js <url> <botName> [debateId]");
    console.log("");
    console.log("Arguments:");
    console.log("  url       - WebSocket URL (ws://host:port/debate)");
    console.log("              or HTTP URL (http://host:port) - will auto-convert to ws://host:port/debate");
    console.log("              or HTTPS URL (https://host:port) - will auto-convert to wss://host:port/debate");
    console.log("  botName   - Name for this bot (e.g., bot_alpha)");
    console.log("  debateId  - (Optional) Debate ID to join. If omitted, platform will assign one.");
    console.log("");
    console.log("Examples:");
    console.log("  node debate_client.js http://localhost:8081 bot_alpha debate-12345");
    console.log("  node debate_client.js http://localhost:8081 bot_alpha");
    console.log("  node debate_client.js https://example.com bot_beta");
    console.log("  node debate_client.js ws://localhost:8081/debate bot_test");
    console.log("");
    console.log("API Endpoints:");
    console.log("  Bot WebSocket:      ws://host:port/debate");
    console.log("  Frontend WebSocket: ws://host:port/frontend");
    console.log("  Create Debate:      POST /api/debate/create");
    console.log("  List Debates:       GET  /api/debates");
    console.log("  Get Debate:         GET  /api/debate/{id}");
    console.log("  Frontend UI:        http://host:port/");
    process.exit(1);
}

new DebateClient(url, botName, debateId).run();
