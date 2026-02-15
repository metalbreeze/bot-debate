// Global state
let currentDebateId = null;
let ws = null;

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    setupEventListeners();
    loadExistingDebates();
});

// Setup event listeners
function setupEventListeners() {
    document.getElementById('create-form').addEventListener('submit', handleCreateDebate);
}

// Handle create debate form submission
async function handleCreateDebate(e) {
    e.preventDefault();

    const topic = document.getElementById('topic').value.trim();
    const rounds = parseInt(document.getElementById('rounds').value);

    if (!topic) {
        alert('请输入辩论主题');
        return;
    }

    try {
        const response = await fetch('/api/debate/create', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                topic: topic,
                total_rounds: rounds,
            }),
        });

        if (!response.ok) {
            throw new Error('Failed to create debate');
        }

        const data = await response.json();
        currentDebateId = data.debate_id;

        // Show debate info
        showDebateInfo(data);

        // Connect to WebSocket
        connectWebSocket(data.debate_id);

        // Clear form
        document.getElementById('create-form').reset();

        // Reload existing debates
        loadExistingDebates();
    } catch (error) {
        console.error('Error creating debate:', error);
        alert('创建辩论失败，请重试');
    }
}

// Show debate info section
function showDebateInfo(data) {
    const infoSection = document.getElementById('debate-info');
    infoSection.style.display = 'block';

    document.getElementById('debate-id').textContent = data.debate_id;
    document.getElementById('debate-topic').textContent = data.topic;
    updateDebateStatus('waiting');
    document.getElementById('current-round').textContent = `1 / ${data.total_rounds}`;

    // Show log section
    document.getElementById('debate-log').style.display = 'block';
    document.getElementById('log-container').innerHTML = '<p class="loading">等待 Bot 连接...</p>';

    // Scroll to debate info
    infoSection.scrollIntoView({ behavior: 'smooth' });
}

// Connect to WebSocket
function connectWebSocket(debateId) {
    if (ws) {
        ws.close();
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/frontend`;

    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log('WebSocket connected');
        // Subscribe to debate
        ws.send(JSON.stringify({
            type: 'subscribe_debate',
            timestamp: new Date().toISOString(),
            data: {
                debate_id: debateId,
            },
        }));
    };

    ws.onmessage = (event) => {
        try {
            const message = JSON.parse(event.data);
            handleWebSocketMessage(message);
        } catch (error) {
            console.error('Error parsing WebSocket message:', error);
        }
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
    };

    ws.onclose = () => {
        console.log('WebSocket closed');
    };
}

// Handle WebSocket messages
function handleWebSocketMessage(message) {
    console.log('Received message:', message);

    switch (message.type) {
        case 'debate_start':
            handleDebateStart(message.data);
            break;
        case 'debate_waiting':
            handleDebateWaiting(message.data);
            break;
        case 'debate_update':
            handleDebateUpdate(message.data);
            break;
        case 'debate_end':
            handleDebateEnd(message.data);
            break;
        case 'pong':
            // Heartbeat response
            break;
        default:
            console.log('Unknown message type:', message.type);
    }
}

// Handle debate waiting (before start)
function handleDebateWaiting(data) {
    updateDebateStatus('waiting');
    updateSidebarStatus(data.debate_id, 'waiting');

    // Show waiting info
    document.getElementById('supporting-bot').textContent = '等待中...';
    document.getElementById('opposing-bot').textContent = '等待中...';
    document.getElementById('current-round').textContent = `0 / ${data.total_rounds}`;

    // Display joined bots
    const logContainer = document.getElementById('log-container');
    if (data.joined_bots && data.joined_bots.length > 0) {
        logContainer.innerHTML = `
            <div class="waiting-info">
                <h3>⏳ 等待 Bot 加入</h3>
                <p>已加入的 Bot (${data.joined_bots.length}/2):</p>
                <ul class="joined-bots-list">
                    ${data.joined_bots.map(bot => `<li>✓ ${bot}</li>`).join('')}
                </ul>
                ${data.joined_bots.length < 2 ? '<p class="waiting-message">等待第二个 Bot 加入...</p>' : '<p class="waiting-message">Bot 已就绪，辩论即将开始...</p>'}
            </div>
        `;
    } else {
        logContainer.innerHTML = `
            <div class="waiting-info">
                <h3>⏳ 等待 Bot 加入</h3>
                <p>暂无 Bot 加入 (0/2)</p>
                <p class="waiting-message">等待 Bot 连接...</p>
            </div>
        `;
    }
}

// Handle debate start
function handleDebateStart(data) {
    updateDebateStatus('active');
    updateSidebarStatus(data.debate_id, 'active');
    document.getElementById('supporting-bot').textContent = data.supporting_side;
    document.getElementById('opposing-bot').textContent = data.opposing_side;
    document.getElementById('current-round').textContent = `${data.current_round} / ${data.total_rounds}`;

    // Clear loading message and show prompt indicator
    const logContainer = document.getElementById('log-container');
    logContainer.innerHTML = '';
    appendPromptIndicator(logContainer, data.next_speaker, data.current_round);
}

// Handle debate update
function handleDebateUpdate(data) {
    updateDebateStatus('active');
    updateSidebarStatus(data.debate_id, 'active');

    if (data.supporting_side) {
        document.getElementById('supporting-bot').textContent = data.supporting_side;
    }
    if (data.opposing_side) {
        document.getElementById('opposing-bot').textContent = data.opposing_side;
    }

    document.getElementById('current-round').textContent = `${data.current_round} / ${data.total_rounds}`;

    // Update debate log
    if (data.debate_log) {
        displayDebateLog(data.debate_log);
    }

    // Show prompt indicator for next speaker
    if (data.next_speaker) {
        const logContainer = document.getElementById('log-container');
        appendPromptIndicator(logContainer, data.next_speaker, data.current_round);
    }
}

// Handle debate end
function handleDebateEnd(data) {
    const endStatus = data.status || 'completed';
    updateDebateStatus(endStatus);
    updateSidebarStatus(data.debate_id, endStatus);

    // Update log one last time
    if (data.debate_log) {
        displayDebateLog(data.debate_log);
    }

    // Show result
    displayResult(data);

    // Close WebSocket
    if (ws) {
        ws.close();
        ws = null;
    }

    // Reload debates list to update status
    loadExistingDebates();
}

// Update debate status badge
function updateDebateStatus(status) {
    const statusBadge = document.getElementById('debate-status');
    statusBadge.className = 'badge';

    switch (status) {
        case 'waiting':
            statusBadge.classList.add('waiting');
            statusBadge.textContent = '等待中';
            break;
        case 'active':
            statusBadge.classList.add('active');
            statusBadge.textContent = '进行中';
            break;
        case 'completed':
            statusBadge.classList.add('completed');
            statusBadge.textContent = '已完成';
            break;
        case 'timeout':
            statusBadge.classList.add('timeout');
            statusBadge.textContent = '已超时';
            break;
        default:
            statusBadge.textContent = status;
    }
}

// Display debate log
function displayDebateLog(debateLog) {
    const container = document.getElementById('log-container');
    container.innerHTML = '';

    debateLog.forEach((entry) => {
        const logEntry = document.createElement('div');
        logEntry.className = `log-entry ${entry.side}`;

        const header = document.createElement('div');
        header.className = 'log-entry-header';

        const speaker = document.createElement('div');
        speaker.className = `log-entry-speaker ${entry.side}`;
        speaker.textContent = entry.speaker;

        const meta = document.createElement('div');
        meta.className = 'log-entry-meta';
        meta.innerHTML = `
            <span>轮次 ${entry.round}</span>
            <span>${new Date(entry.timestamp).toLocaleString('zh-CN')}</span>
        `;

        header.appendChild(speaker);
        header.appendChild(meta);

        const content = document.createElement('div');
        content.className = 'log-entry-content';
        content.innerHTML = marked.parse(entry.message.content);

        logEntry.appendChild(header);
        logEntry.appendChild(content);

        container.appendChild(logEntry);
    });

    // Scroll to bottom
    container.scrollTop = container.scrollHeight;
}

// Display result
function displayResult(data) {
    const resultSection = document.getElementById('result-section');
    const resultContainer = document.getElementById('result-container');

    resultSection.style.display = 'block';

    const result = data.debate_result;

    // Create scores display
    const scoresDiv = document.createElement('div');
    scoresDiv.className = 'result-scores';

    const supportingBox = document.createElement('div');
    supportingBox.className = 'score-box';
    if (result.winner === 'supporting') {
        supportingBox.classList.add('winner');
    }
    supportingBox.innerHTML = `
        <h3>正方: ${data.supporting_side}</h3>
        <div class="score">${result.supporting_score}</div>
        ${result.winner === 'supporting' ? '<p>🏆 获胜</p>' : ''}
    `;

    const opposingBox = document.createElement('div');
    opposingBox.className = 'score-box';
    if (result.winner === 'opposing') {
        opposingBox.classList.add('winner');
    }
    opposingBox.innerHTML = `
        <h3>反方: ${data.opposing_side}</h3>
        <div class="score">${result.opposing_score}</div>
        ${result.winner === 'opposing' ? '<p>🏆 获胜</p>' : ''}
    `;

    scoresDiv.appendChild(supportingBox);
    scoresDiv.appendChild(opposingBox);

    // Create summary display
    const summaryDiv = document.createElement('div');
    summaryDiv.className = 'result-summary';
    summaryDiv.innerHTML = marked.parse(result.summary.content);

    resultContainer.innerHTML = '';
    resultContainer.appendChild(scoresDiv);
    resultContainer.appendChild(summaryDiv);

    // Scroll to result
    resultSection.scrollIntoView({ behavior: 'smooth' });
}

// Load existing debates
async function loadExistingDebates() {
    try {
        const response = await fetch('/api/debates');
        if (!response.ok) {
            throw new Error('Failed to load debates');
        }

        const debates = await response.json();
        displayExistingDebates(debates);
    } catch (error) {
        console.error('Error loading debates:', error);
        document.getElementById('debates-list').innerHTML = '<p class="loading">加载失败</p>';
    }
}

// Display existing debates
function displayExistingDebates(debates) {
    const container = document.getElementById('debates-list');

    if (!debates || debates.length === 0) {
        container.innerHTML = '<p class="loading">暂无辩论记录</p>';
        return;
    }

    container.innerHTML = '';

    debates.forEach((debate) => {
        const item = document.createElement('div');
        item.className = 'debate-item';
        item.setAttribute('data-debate-id', debate.debate_id);
        item.onclick = () => viewDebate(debate.debate_id, item);

        const header = document.createElement('div');
        header.className = 'debate-item-header';

        const topic = document.createElement('div');
        topic.className = 'debate-item-topic';
        topic.textContent = debate.topic;

        const status = document.createElement('span');
        status.className = 'badge';
        switch (debate.status) {
            case 'waiting':
                status.classList.add('waiting');
                status.textContent = '等待中';
                break;
            case 'active':
                status.classList.add('active');
                status.textContent = '进行中';
                break;
            case 'completed':
                status.classList.add('completed');
                status.textContent = '已完成';
                break;
            case 'timeout':
                status.classList.add('timeout');
                status.textContent = '已超时';
                break;
            default:
                status.textContent = debate.status;
        }

        header.appendChild(topic);
        header.appendChild(status);

        const meta = document.createElement('div');
        meta.className = 'debate-item-meta';
        meta.textContent = `创建于: ${new Date(debate.created_at).toLocaleString('zh-CN')} | 轮次: ${debate.total_rounds}`;

        item.appendChild(header);
        item.appendChild(meta);

        container.appendChild(item);
    });
}

// Helper function to check if screen is mobile
function isMobileView() {
    return window.innerWidth < 768;
}

// View existing debate
function viewDebate(debateId, clickedItem) {
    currentDebateId = debateId;

    // Load debate details
    fetch(`/api/debate/${debateId}`)
        .then(response => response.json())
        .then(data => {
            if (isMobileView()) {
                // Mobile: Accordion style - show details under clicked item
                displayDebateMobile(data, clickedItem);
            } else {
                // Desktop: Show in fixed right panel
                displayDebateDesktop(data);
            }

            // Connect WebSocket if active
            if (data.debate.status === 'active' || data.debate.status === 'waiting') {
                connectWebSocket(debateId);
            }
        })
        .catch(error => {
            console.error('Error loading debate:', error);
            alert('加载辩论失败');
        });
}

// Display debate details on mobile (accordion under clicked item)
function displayDebateMobile(data, clickedItem) {
    // Remove any existing expanded details in the list
    const existingDetails = document.querySelectorAll('.debate-item-details');
    existingDetails.forEach(detail => detail.remove());

    // Remove active class from all items
    const allItems = document.querySelectorAll('.debate-item');
    allItems.forEach(item => item.classList.remove('active'));

    // Add active class to clicked item
    clickedItem.classList.add('active');

    // Create details element
    const detailsDiv = document.createElement('div');
    detailsDiv.className = 'debate-item-details';

    // Build the details content
    detailsDiv.innerHTML = buildDebateDetailsHTML(data);

    // Insert after clicked item
    clickedItem.insertAdjacentElement('afterend', detailsDiv);

    // Smooth scroll to the clicked item
    setTimeout(() => {
        clickedItem.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }, 100);
}

// Display debate details on desktop (fixed right panel)
function displayDebateDesktop(data) {
    // Hide placeholder, show detail panel
    document.getElementById('detail-placeholder').style.display = 'none';
    document.getElementById('debate-info').style.display = 'block';
    document.getElementById('debate-log').style.display = 'block';

    // Populate the fixed panel
    document.getElementById('debate-id').textContent = data.debate.debate_id;
    document.getElementById('debate-topic').textContent = data.debate.topic;
    updateDebateStatus(data.debate.status);
    document.getElementById('current-round').textContent = `${data.debate.current_round} / ${data.debate.total_rounds}`;

    // Show bots
    const bots = data.bots || [];
    const supportingBot = bots.find(b => b.side === 'supporting');
    const opposingBot = bots.find(b => b.side === 'opposing');

    document.getElementById('supporting-bot').textContent = supportingBot ? supportingBot.bot_identifier : '等待连接...';
    document.getElementById('opposing-bot').textContent = opposingBot ? opposingBot.bot_identifier : '等待连接...';

    // Show log
    if (data.debate_log && data.debate_log.length > 0) {
        displayDebateLog(data.debate_log);
    } else {
        document.getElementById('log-container').innerHTML = '<p class="loading">暂无发言记录</p>';
    }

    // Show result if completed or timeout
    if ((data.debate.status === 'completed' || data.debate.status === 'timeout') && data.result) {
        displayResult({
            supporting_side: supportingBot?.bot_identifier,
            opposing_side: opposingBot?.bot_identifier,
            debate_result: data.result,
        });
    } else {
        document.getElementById('result-section').style.display = 'none';
    }

    // Highlight active item in sidebar
    const allItems = document.querySelectorAll('.debate-item');
    allItems.forEach(item => {
        if (item.getAttribute('data-debate-id') === data.debate.debate_id) {
            item.classList.add('active');
        } else {
            item.classList.remove('active');
        }
    });
}

// Build HTML content for debate details (used in mobile view)
function buildDebateDetailsHTML(data) {
    const bots = data.bots || [];
    const supportingBot = bots.find(b => b.side === 'supporting');
    const opposingBot = bots.find(b => b.side === 'opposing');

    let statusBadgeClass = '';
    let statusText = '';
    switch (data.debate.status) {
        case 'waiting':
            statusBadgeClass = 'waiting';
            statusText = '等待中';
            break;
        case 'active':
            statusBadgeClass = 'active';
            statusText = '进行中';
            break;
        case 'completed':
            statusBadgeClass = 'completed';
            statusText = '已完成';
            break;
        case 'timeout':
            statusBadgeClass = 'timeout';
            statusText = '已超时';
            break;
        default:
            statusText = data.debate.status;
    }

    let html = `
        <div class="mobile-debate-header">
            <h3>${data.debate.topic}</h3>
            <span class="badge ${statusBadgeClass}">${statusText}</span>
        </div>
        <div class="mobile-debate-info">
            <p><strong>辩论 ID:</strong> ${data.debate.debate_id}</p>
            <p><strong>轮次:</strong> ${data.debate.current_round} / ${data.debate.total_rounds}</p>
            <p><strong>正方:</strong> ${supportingBot ? supportingBot.bot_identifier : '等待连接...'}</p>
            <p><strong>反方:</strong> ${opposingBot ? opposingBot.bot_identifier : '等待连接...'}</p>
        </div>
    `;

    // Add debate log
    if (data.debate_log && data.debate_log.length > 0) {
        html += '<div class="mobile-debate-log"><h4>辩论记录</h4>';
        data.debate_log.forEach((entry) => {
            html += `
                <div class="log-entry ${entry.side}">
                    <div class="log-entry-header">
                        <div class="log-entry-speaker ${entry.side}">${entry.speaker}</div>
                        <div class="log-entry-meta">
                            <span>轮次 ${entry.round}</span>
                            <span>${new Date(entry.timestamp).toLocaleString('zh-CN')}</span>
                        </div>
                    </div>
                    <div class="log-entry-content">${marked.parse(entry.message.content)}</div>
                </div>
            `;
        });
        html += '</div>';
    } else {
        html += '<div class="mobile-debate-log"><p class="loading">暂无发言记录</p></div>';
    }

    // Add result if completed or timeout
    if ((data.debate.status === 'completed' || data.debate.status === 'timeout') && data.result) {
        const result = data.result;
        html += `
            <div class="mobile-debate-result">
                <h4>辩论结果</h4>
                <div class="result-scores">
                    <div class="score-box ${result.winner === 'supporting' ? 'winner' : ''}">
                        <h3>正方: ${supportingBot?.bot_identifier}</h3>
                        <div class="score">${result.supporting_score}</div>
                        ${result.winner === 'supporting' ? '<p>🏆 获胜</p>' : ''}
                    </div>
                    <div class="score-box ${result.winner === 'opposing' ? 'winner' : ''}">
                        <h3>反方: ${opposingBot?.bot_identifier}</h3>
                        <div class="score">${result.opposing_score}</div>
                        ${result.winner === 'opposing' ? '<p>🏆 获胜</p>' : ''}
                    </div>
                </div>
                <div class="result-summary">${marked.parse(result.summary.content)}</div>
            </div>
        `;
    }

    return html;
}

// Copy debate ID to clipboard
function copyDebateId() {
    const debateId = document.getElementById('debate-id').textContent;
    navigator.clipboard.writeText(debateId).then(() => {
        alert('辩论 ID 已复制到剪贴板');
    }).catch(err => {
        console.error('Failed to copy:', err);
        alert('复制失败');
    });
}

// Append prompt/waiting indicator to log container
function appendPromptIndicator(container, nextSpeaker, currentRound) {
    // Remove any existing indicator
    const existing = container.querySelector('.prompt-indicator');
    if (existing) existing.remove();

    const indicator = document.createElement('div');
    indicator.className = 'prompt-indicator';
    indicator.innerHTML = `
        <div class="prompt-indicator-content">
            <span class="prompt-pulse"></span>
            <span class="prompt-text">轮次 ${currentRound} — 已发送 Prompt 至 <strong>${nextSpeaker}</strong>，等待 Reply...</span>
        </div>
    `;
    container.appendChild(indicator);
    container.scrollTop = container.scrollHeight;
}

// Update sidebar debate item status badge in real-time
function updateSidebarStatus(debateId, status) {
    if (!debateId) return;
    const item = document.querySelector(`.debate-item[data-debate-id="${debateId}"]`);
    if (!item) return;

    const badge = item.querySelector('.badge');
    if (!badge) return;

    badge.className = 'badge';
    switch (status) {
        case 'waiting':
            badge.classList.add('waiting');
            badge.textContent = '等待中';
            break;
        case 'active':
            badge.classList.add('active');
            badge.textContent = '进行中';
            break;
        case 'completed':
            badge.classList.add('completed');
            badge.textContent = '已完成';
            break;
        case 'timeout':
            badge.classList.add('timeout');
            badge.textContent = '已超时';
            break;
        default:
            badge.textContent = status;
    }
}

// Send heartbeat every 30 seconds
setInterval(() => {
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
            type: 'ping',
            timestamp: new Date().toISOString(),
            data: {},
        }));
    }
}, 30000);
