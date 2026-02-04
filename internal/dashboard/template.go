/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dashboard

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Team Health Dashboard - kube-assist</title>
    <style>
        :root {
            --bg-primary: #0f172a;
            --bg-secondary: #1e293b;
            --bg-card: #334155;
            --text-primary: #f8fafc;
            --text-secondary: #94a3b8;
            --accent-blue: #3b82f6;
            --critical: #ef4444;
            --warning: #f59e0b;
            --info: #3b82f6;
            --success: #22c55e;
        }

        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            min-height: 100vh;
        }

        .header {
            background: var(--bg-secondary);
            padding: 1rem 2rem;
            display: flex;
            justify-content: space-between;
            align-items: center;
            border-bottom: 1px solid var(--bg-card);
        }

        .header h1 {
            font-size: 1.5rem;
            font-weight: 600;
        }

        .header .status {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            font-size: 0.875rem;
            color: var(--text-secondary);
        }

        .status-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
            background: var(--success);
            animation: pulse 2s infinite;
        }

        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }

        .container {
            max-width: 1400px;
            margin: 0 auto;
            padding: 2rem;
        }

        .summary-cards {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 1rem;
            margin-bottom: 2rem;
        }

        .summary-card {
            background: var(--bg-secondary);
            border-radius: 12px;
            padding: 1.5rem;
            border: 1px solid var(--bg-card);
        }

        .summary-card .label {
            font-size: 0.875rem;
            color: var(--text-secondary);
            margin-bottom: 0.5rem;
        }

        .summary-card .value {
            font-size: 2.5rem;
            font-weight: 700;
        }

        .summary-card.critical .value { color: var(--critical); }
        .summary-card.warning .value { color: var(--warning); }
        .summary-card.healthy .value { color: var(--success); }

        .checkers-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
            gap: 1.5rem;
        }

        .checker-card {
            background: var(--bg-secondary);
            border-radius: 12px;
            border: 1px solid var(--bg-card);
            overflow: hidden;
        }

        .checker-header {
            padding: 1rem 1.5rem;
            display: flex;
            justify-content: space-between;
            align-items: center;
            border-bottom: 1px solid var(--bg-card);
        }

        .checker-name {
            font-weight: 600;
            text-transform: capitalize;
        }

        .checker-stats {
            display: flex;
            gap: 1rem;
            font-size: 0.875rem;
        }

        .checker-stats span {
            display: flex;
            align-items: center;
            gap: 0.25rem;
        }

        .issues-list {
            max-height: 400px;
            overflow-y: auto;
        }

        .issue {
            padding: 1rem 1.5rem;
            border-bottom: 1px solid var(--bg-card);
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
        }

        .issue:last-child {
            border-bottom: none;
        }

        .issue-header {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .severity-badge {
            padding: 0.25rem 0.5rem;
            border-radius: 4px;
            font-size: 0.75rem;
            font-weight: 600;
            text-transform: uppercase;
        }

        .severity-badge.critical {
            background: rgba(239, 68, 68, 0.2);
            color: var(--critical);
        }

        .severity-badge.warning {
            background: rgba(245, 158, 11, 0.2);
            color: var(--warning);
        }

        .severity-badge.info {
            background: rgba(59, 130, 246, 0.2);
            color: var(--info);
        }

        .issue-type {
            font-weight: 500;
        }

        .issue-resource {
            font-family: monospace;
            font-size: 0.875rem;
            color: var(--text-secondary);
        }

        .issue-message {
            font-size: 0.875rem;
            color: var(--text-secondary);
        }

        .issue-suggestion {
            font-size: 0.75rem;
            color: var(--accent-blue);
            margin-top: 0.25rem;
        }

        .no-issues {
            padding: 2rem;
            text-align: center;
            color: var(--text-secondary);
        }

        .timestamp {
            text-align: center;
            padding: 1rem;
            font-size: 0.75rem;
            color: var(--text-secondary);
        }

        .refresh-btn {
            background: var(--accent-blue);
            color: white;
            border: none;
            padding: 0.5rem 1rem;
            border-radius: 6px;
            cursor: pointer;
            font-size: 0.875rem;
        }

        .refresh-btn:hover {
            opacity: 0.9;
        }

        .connecting {
            text-align: center;
            padding: 4rem 2rem;
            color: var(--text-secondary);
        }

        .spinner {
            width: 40px;
            height: 40px;
            border: 3px solid var(--bg-card);
            border-top-color: var(--accent-blue);
            border-radius: 50%;
            animation: spin 1s linear infinite;
            margin: 0 auto 1rem;
        }

        @keyframes spin {
            to { transform: rotate(360deg); }
        }
    </style>
</head>
<body>
    <header class="header">
        <h1>Team Health Dashboard</h1>
        <div class="status">
            <div class="status-dot" id="connectionDot"></div>
            <span id="connectionStatus">Connecting...</span>
            <button class="refresh-btn" onclick="triggerCheck()">Refresh</button>
        </div>
    </header>

    <main class="container">
        <div id="loading" class="connecting">
            <div class="spinner"></div>
            <p>Connecting to health monitor...</p>
        </div>

        <div id="dashboard" style="display: none;">
            <div class="summary-cards">
                <div class="summary-card healthy">
                    <div class="label">Healthy Resources</div>
                    <div class="value" id="healthyCount">0</div>
                </div>
                <div class="summary-card critical">
                    <div class="label">Critical Issues</div>
                    <div class="value" id="criticalCount">0</div>
                </div>
                <div class="summary-card warning">
                    <div class="label">Warnings</div>
                    <div class="value" id="warningCount">0</div>
                </div>
                <div class="summary-card">
                    <div class="label">Info</div>
                    <div class="value" id="infoCount">0</div>
                </div>
            </div>

            <div class="checkers-grid" id="checkersGrid">
                <!-- Checker cards will be inserted here -->
            </div>

            <div class="timestamp" id="timestamp"></div>
        </div>
    </main>

    <script>
        let eventSource = null;

        function connect() {
            eventSource = new EventSource('/api/events');

            eventSource.onopen = () => {
                document.getElementById('connectionDot').style.background = '#22c55e';
                document.getElementById('connectionStatus').textContent = 'Connected';
            };

            eventSource.onmessage = (event) => {
                const data = JSON.parse(event.data);
                updateDashboard(data);
            };

            eventSource.onerror = () => {
                document.getElementById('connectionDot').style.background = '#ef4444';
                document.getElementById('connectionStatus').textContent = 'Disconnected';
                setTimeout(connect, 5000);
            };
        }

        function updateDashboard(data) {
            document.getElementById('loading').style.display = 'none';
            document.getElementById('dashboard').style.display = 'block';

            // Update summary
            document.getElementById('healthyCount').textContent = data.summary.totalHealthy;
            document.getElementById('criticalCount').textContent = data.summary.criticalCount;
            document.getElementById('warningCount').textContent = data.summary.warningCount;
            document.getElementById('infoCount').textContent = data.summary.infoCount;

            // Update checkers
            const grid = document.getElementById('checkersGrid');
            grid.innerHTML = '';

            for (const [name, result] of Object.entries(data.results)) {
                const card = createCheckerCard(name, result);
                grid.appendChild(card);
            }

            // Update timestamp
            const ts = new Date(data.timestamp);
            document.getElementById('timestamp').textContent =
                'Last updated: ' + ts.toLocaleTimeString();
        }

        function createCheckerCard(name, result) {
            const card = document.createElement('div');
            card.className = 'checker-card';

            const criticalCount = result.issues.filter(i => i.severity === 'critical').length;
            const warningCount = result.issues.filter(i => i.severity === 'warning').length;

            card.innerHTML = ` + "`" + `
                <div class="checker-header">
                    <span class="checker-name">${name}</span>
                    <div class="checker-stats">
                        <span style="color: #22c55e;">${result.healthy} healthy</span>
                        ${criticalCount > 0 ? ` + "`" + `<span style="color: #ef4444;">${criticalCount} critical</span>` + "`" + ` : ''}
                        ${warningCount > 0 ? ` + "`" + `<span style="color: #f59e0b;">${warningCount} warning</span>` + "`" + ` : ''}
                    </div>
                </div>
                <div class="issues-list">
                    ${result.issues.length === 0 ?
                        '<div class="no-issues">All resources healthy</div>' :
                        result.issues.map(issue => createIssueHTML(issue)).join('')
                    }
                </div>
            ` + "`" + `;

            return card;
        }

        function createIssueHTML(issue) {
            return ` + "`" + `
                <div class="issue">
                    <div class="issue-header">
                        <span class="severity-badge ${issue.severity}">${issue.severity}</span>
                        <span class="issue-type">${issue.type}</span>
                    </div>
                    <div class="issue-resource">${issue.namespace}/${issue.resource}</div>
                    <div class="issue-message">${issue.message}</div>
                    ${issue.suggestion ? ` + "`" + `<div class="issue-suggestion">Suggestion: ${issue.suggestion}</div>` + "`" + ` : ''}
                </div>
            ` + "`" + `;
        }

        async function triggerCheck() {
            try {
                await fetch('/api/check', { method: 'POST' });
            } catch (e) {
                console.error('Failed to trigger check:', e);
            }
        }

        // Start connection
        connect();
    </script>
</body>
</html>
`
