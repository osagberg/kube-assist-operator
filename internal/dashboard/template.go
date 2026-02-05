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
<html lang="en" data-theme="dark">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>kube-assist - Cluster Health Dashboard</title>
    <style>
        /* ========================================
           CSS Variables - Dark Theme (Default)
           ======================================== */
        :root {
            --bg-primary: #0b0f1a;
            --bg-secondary: #111827;
            --bg-card: #1f2937;
            --bg-card-elevated: #263046;
            --bg-hover: #374151;
            --bg-input: #1a2332;
            --text-primary: #f9fafb;
            --text-secondary: #9ca3af;
            --text-muted: #6b7280;
            --accent: #6366f1;
            --accent-light: #818cf8;
            --accent-subtle: rgba(99, 102, 241, 0.12);
            --critical: #ef4444;
            --critical-bg: rgba(239, 68, 68, 0.12);
            --critical-border: rgba(239, 68, 68, 0.3);
            --warning: #f59e0b;
            --warning-bg: rgba(245, 158, 11, 0.12);
            --warning-border: rgba(245, 158, 11, 0.3);
            --info: #3b82f6;
            --info-bg: rgba(59, 130, 246, 0.12);
            --info-border: rgba(59, 130, 246, 0.3);
            --success: #10b981;
            --success-bg: rgba(16, 185, 129, 0.12);
            --success-border: rgba(16, 185, 129, 0.3);
            --border: rgba(255, 255, 255, 0.08);
            --border-strong: rgba(255, 255, 255, 0.15);
            --shadow-sm: 0 1px 2px rgba(0,0,0,0.3);
            --shadow-md: 0 4px 12px rgba(0,0,0,0.4);
            --shadow-lg: 0 8px 30px rgba(0,0,0,0.5);
            --radius-sm: 6px;
            --radius-md: 10px;
            --radius-lg: 14px;
            --radius-xl: 20px;
            --font-mono: 'SF Mono', 'Cascadia Code', 'Fira Code', 'JetBrains Mono', Consolas, monospace;
            --font-sans: -apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Roboto, sans-serif;
        }

        /* ========================================
           Light Theme
           ======================================== */
        [data-theme="light"] {
            --bg-primary: #f8fafc;
            --bg-secondary: #ffffff;
            --bg-card: #f1f5f9;
            --bg-card-elevated: #e8edf4;
            --bg-hover: #e2e8f0;
            --bg-input: #f1f5f9;
            --text-primary: #0f172a;
            --text-secondary: #475569;
            --text-muted: #94a3b8;
            --accent: #4f46e5;
            --accent-light: #6366f1;
            --accent-subtle: rgba(79, 70, 229, 0.08);
            --border: rgba(0, 0, 0, 0.08);
            --border-strong: rgba(0, 0, 0, 0.15);
            --shadow-sm: 0 1px 2px rgba(0,0,0,0.06);
            --shadow-md: 0 4px 12px rgba(0,0,0,0.08);
            --shadow-lg: 0 8px 30px rgba(0,0,0,0.12);
        }

        /* ========================================
           Reset & Base
           ======================================== */
        *, *::before, *::after { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: var(--font-sans);
            background: var(--bg-primary);
            color: var(--text-primary);
            min-height: 100vh;
            line-height: 1.5;
            -webkit-font-smoothing: antialiased;
            transition: background 0.25s ease, color 0.25s ease;
        }

        /* ========================================
           Animations
           ======================================== */
        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.4} }
        @keyframes spin { to{transform:rotate(360deg)} }
        @keyframes fade-in { from{opacity:0;transform:translateY(6px)} to{opacity:1;transform:translateY(0)} }
        @keyframes slide-up { from{opacity:0;transform:translateY(12px)} to{opacity:1;transform:translateY(0)} }
        @keyframes shimmer { 0%{background-position:200% 0} 100%{background-position:-200% 0} }
        @keyframes ring-fill { 0%{stroke-dashoffset:283} }
        @keyframes toast-in { from{opacity:0;transform:translateY(20px)} to{opacity:1;transform:translateY(0)} }
        @keyframes toast-out { from{opacity:1;transform:translateY(0)} to{opacity:0;transform:translateY(20px)} }

        .animate-in { animation: fade-in 0.3s ease forwards; }

        /* ========================================
           Topbar
           ======================================== */
        .topbar {
            height: 56px;
            background: var(--bg-secondary);
            border-bottom: 1px solid var(--border);
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0 24px;
            position: sticky;
            top: 0;
            z-index: 100;
            backdrop-filter: blur(16px);
            -webkit-backdrop-filter: blur(16px);
        }
        .topbar-left { display: flex; align-items: center; gap: 12px; }
        .topbar-logo {
            width: 28px; height: 28px;
            background: linear-gradient(135deg, var(--accent), #a78bfa);
            border-radius: var(--radius-sm);
            display: flex; align-items: center; justify-content: center;
        }
        .topbar-logo svg { width: 16px; height: 16px; color: white; }
        .topbar-title {
            font-size: 15px; font-weight: 600; letter-spacing: -0.01em;
        }
        .topbar-badge {
            font-size: 11px; font-weight: 600; padding: 2px 10px;
            border-radius: 12px; text-transform: uppercase; letter-spacing: 0.04em;
        }
        .topbar-badge.ok { background: var(--success-bg); color: var(--success); border: 1px solid var(--success-border); }
        .topbar-badge.warn { background: var(--warning-bg); color: var(--warning); border: 1px solid var(--warning-border); }
        .topbar-badge.crit { background: var(--critical-bg); color: var(--critical); border: 1px solid var(--critical-border); animation: pulse 2s infinite; }
        .topbar-right { display: flex; align-items: center; gap: 8px; }
        .topbar-status { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-muted); margin-right: 8px; }
        .topbar-dot {
            width: 7px; height: 7px; border-radius: 50%;
            background: var(--success); animation: pulse 2s infinite;
        }
        .topbar-dot.off { background: var(--critical); animation: none; }
        .topbar-dot.paused { background: var(--warning); animation: none; }

        /* ========================================
           Buttons
           ======================================== */
        .btn {
            display: inline-flex; align-items: center; gap: 6px;
            padding: 7px 14px; border-radius: var(--radius-sm);
            font-size: 13px; font-weight: 500; cursor: pointer;
            border: 1px solid var(--border); background: var(--bg-card);
            color: var(--text-primary); transition: all 0.15s ease;
            white-space: nowrap;
        }
        .btn:hover { background: var(--bg-hover); border-color: var(--border-strong); }
        .btn svg { width: 15px; height: 15px; flex-shrink: 0; }
        .btn-icon { padding: 7px; }
        .btn-accent {
            background: var(--accent); color: white; border-color: var(--accent);
        }
        .btn-accent:hover { opacity: 0.9; background: var(--accent); border-color: var(--accent); }
        .btn-sm { padding: 4px 10px; font-size: 12px; }
        .btn-sm svg { width: 13px; height: 13px; }

        /* ========================================
           Layout
           ======================================== */
        .main { max-width: 1440px; margin: 0 auto; padding: 20px 24px; }

        /* ========================================
           Loading / Skeleton
           ======================================== */
        .loading-screen {
            display: flex; flex-direction: column; align-items: center;
            justify-content: center; padding: 80px 20px; color: var(--text-muted);
        }
        .loading-spinner {
            width: 40px; height: 40px; border: 3px solid var(--border);
            border-top-color: var(--accent); border-radius: 50%;
            animation: spin 0.8s linear infinite; margin-bottom: 16px;
        }
        .skeleton {
            background: linear-gradient(90deg, var(--bg-card) 0%, var(--bg-hover) 50%, var(--bg-card) 100%);
            background-size: 200% 100%; animation: shimmer 1.5s ease-in-out infinite;
            border-radius: var(--radius-md);
        }

        /* ========================================
           Toolbar
           ======================================== */
        .toolbar {
            display: flex; flex-wrap: wrap; gap: 10px; margin-bottom: 20px;
            padding: 12px 16px; background: var(--bg-secondary);
            border-radius: var(--radius-lg); border: 1px solid var(--border);
            align-items: center;
        }
        .search-wrap {
            flex: 1; min-width: 200px; position: relative;
        }
        .search-wrap svg {
            position: absolute; left: 10px; top: 50%; transform: translateY(-50%);
            width: 15px; height: 15px; color: var(--text-muted); pointer-events: none;
        }
        .search-wrap input {
            width: 100%; padding: 8px 12px 8px 34px;
            background: var(--bg-input); border: 1px solid var(--border);
            border-radius: var(--radius-sm); color: var(--text-primary);
            font-size: 13px; transition: border-color 0.15s;
        }
        .search-wrap input:focus { outline: none; border-color: var(--accent); }
        .search-wrap input::placeholder { color: var(--text-muted); }
        .filter-select {
            padding: 8px 28px 8px 10px; background: var(--bg-input);
            border: 1px solid var(--border); border-radius: var(--radius-sm);
            color: var(--text-primary); font-size: 13px; cursor: pointer;
            appearance: none;
            background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10' viewBox='0 0 24 24' fill='none' stroke='%236b7280' stroke-width='2.5'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E");
            background-repeat: no-repeat; background-position: right 8px center;
        }
        .filter-select:focus { outline: none; border-color: var(--accent); }
        .severity-tabs {
            display: flex; gap: 2px; padding: 3px;
            background: var(--bg-card); border-radius: var(--radius-sm);
        }
        .sev-tab {
            padding: 6px 12px; border: none; background: transparent;
            color: var(--text-muted); font-size: 12px; font-weight: 500;
            border-radius: 4px; cursor: pointer; transition: all 0.15s;
            display: flex; align-items: center; gap: 5px;
        }
        .sev-tab:hover { color: var(--text-secondary); background: var(--bg-hover); }
        .sev-tab.active { background: var(--bg-secondary); color: var(--text-primary); }
        .sev-tab .cnt {
            font-size: 10px; padding: 1px 6px; border-radius: 8px;
            background: var(--bg-hover); font-weight: 600;
        }
        .sev-tab.active .cnt { background: var(--accent); color: white; }
        .sev-tab[data-severity="critical"].active .cnt { background: var(--critical); }
        .sev-tab[data-severity="warning"].active .cnt { background: var(--warning); }
        .sev-tab[data-severity="info"].active .cnt { background: var(--info); }
        .toolbar-right { display: flex; gap: 6px; margin-left: auto; }

        /* ========================================
           Stats Row
           ======================================== */
        .stats-row {
            display: grid; grid-template-columns: 200px 1fr; gap: 16px;
            margin-bottom: 20px;
        }
        .score-card {
            background: var(--bg-secondary); border-radius: var(--radius-lg);
            border: 1px solid var(--border); padding: 20px;
            display: flex; flex-direction: column; align-items: center;
            justify-content: center; gap: 8px;
        }
        .ring-wrap { position: relative; width: 110px; height: 110px; }
        .ring-svg { transform: rotate(-90deg); }
        .ring-bg { fill: none; stroke: var(--bg-card); stroke-width: 7; }
        .ring-fill {
            fill: none; stroke: var(--success); stroke-width: 7;
            stroke-linecap: round; stroke-dasharray: 283; stroke-dashoffset: 283;
            transition: stroke-dashoffset 0.8s ease, stroke 0.3s;
        }
        .ring-fill.warn { stroke: var(--warning); }
        .ring-fill.crit { stroke: var(--critical); }
        .ring-text {
            position: absolute; top: 50%; left: 50%;
            transform: translate(-50%, -50%); text-align: center;
        }
        .ring-val { font-size: 28px; font-weight: 700; line-height: 1; }
        .ring-lbl { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
        .score-title { font-size: 12px; color: var(--text-muted); font-weight: 500; }
        .metric-cards { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
        .metric-card {
            background: var(--bg-secondary); border-radius: var(--radius-md);
            border: 1px solid var(--border); padding: 16px 18px;
            transition: transform 0.15s, box-shadow 0.15s;
        }
        .metric-card:hover { transform: translateY(-1px); box-shadow: var(--shadow-md); }
        .metric-label {
            font-size: 11px; color: var(--text-muted); text-transform: uppercase;
            letter-spacing: 0.06em; margin-bottom: 6px; font-weight: 500;
        }
        .metric-val { font-size: 28px; font-weight: 700; line-height: 1.1; }
        .metric-card.crit { border-left: 3px solid var(--critical); }
        .metric-card.crit .metric-val { color: var(--critical); }
        .metric-card.warn { border-left: 3px solid var(--warning); }
        .metric-card.warn .metric-val { color: var(--warning); }
        .metric-card.info-c { border-left: 3px solid var(--info); }
        .metric-card.info-c .metric-val { color: var(--info); }
        .metric-card.ok { border-left: 3px solid var(--success); }
        .metric-card.ok .metric-val { color: var(--success); }

        /* ========================================
           Timeline
           ======================================== */
        .timeline-row {
            margin-bottom: 20px; padding: 12px 16px;
            background: var(--bg-secondary); border-radius: var(--radius-md);
            border: 1px solid var(--border);
        }
        .timeline-title {
            font-size: 11px; color: var(--text-muted); text-transform: uppercase;
            letter-spacing: 0.06em; margin-bottom: 8px; font-weight: 500;
        }
        .timeline {
            display: flex; gap: 6px; align-items: flex-end; height: 48px;
        }
        .tl-bar {
            flex: 1; display: flex; flex-direction: column;
            align-items: center; gap: 4px;
        }
        .tl-fill {
            width: 100%; border-radius: 3px 3px 0 0;
            display: flex; flex-direction: column-reverse;
        }
        .tl-seg { width: 100%; transition: height 0.3s; }
        .tl-seg.c { background: var(--critical); }
        .tl-seg.w { background: var(--warning); }
        .tl-seg.i { background: var(--info); }
        .tl-seg.h { background: var(--success); opacity: 0.25; }
        .tl-time { font-size: 9px; color: var(--text-muted); }

        /* ========================================
           Namespace Tags
           ======================================== */
        .ns-row {
            margin-bottom: 20px; padding: 10px 16px;
            background: var(--bg-secondary); border-radius: var(--radius-sm);
            border: 1px solid var(--border);
            display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
        }
        .ns-label { font-size: 12px; color: var(--text-muted); }
        .ns-tag {
            font-size: 11px; font-family: var(--font-mono);
            background: var(--bg-card); padding: 2px 8px;
            border-radius: 4px; color: var(--text-secondary);
        }

        /* ========================================
           Checker Cards (Grouped Issues)
           ======================================== */
        .checkers { display: grid; grid-template-columns: repeat(auto-fit, minmax(480px, 1fr)); gap: 16px; }
        .chk-card {
            background: var(--bg-secondary); border-radius: var(--radius-lg);
            border: 1px solid var(--border); overflow: hidden;
            transition: box-shadow 0.2s;
        }
        .chk-card:hover { box-shadow: var(--shadow-md); }
        .chk-card.has-crit { border-color: var(--critical-border); }
        .chk-head {
            padding: 14px 18px; display: flex; justify-content: space-between;
            align-items: center; border-bottom: 1px solid var(--border);
            cursor: pointer; user-select: none; transition: background 0.15s;
        }
        .chk-head:hover { background: var(--bg-card); }
        .chk-head-left { display: flex; align-items: center; gap: 10px; }
        .chk-icon {
            width: 30px; height: 30px; border-radius: var(--radius-sm);
            display: flex; align-items: center; justify-content: center;
            background: var(--accent-subtle);
        }
        .chk-icon svg { width: 16px; height: 16px; color: var(--accent-light); }
        .chk-name { font-weight: 600; font-size: 14px; text-transform: capitalize; }
        .chk-head-right { display: flex; align-items: center; gap: 8px; }
        .chk-stat {
            font-size: 11px; padding: 2px 8px; border-radius: 4px;
            font-weight: 600;
        }
        .chk-stat.ok { background: var(--success-bg); color: var(--success); }
        .chk-stat.c { background: var(--critical-bg); color: var(--critical); }
        .chk-stat.w { background: var(--warning-bg); color: var(--warning); }
        .chk-stat.i { background: var(--info-bg); color: var(--info); }
        .chk-chevron {
            transition: transform 0.2s; color: var(--text-muted);
        }
        .chk-card.collapsed .chk-chevron { transform: rotate(-90deg); }
        .chk-body { max-height: 500px; overflow-y: auto; transition: max-height 0.3s, opacity 0.2s; }
        .chk-card.collapsed .chk-body { max-height: 0; opacity: 0; overflow: hidden; }

        /* ========================================
           Issue Items
           ======================================== */
        .issue {
            padding: 14px 18px; border-bottom: 1px solid var(--border);
            animation: fade-in 0.2s ease;
        }
        .issue:last-child { border-bottom: none; }
        .issue:hover { background: rgba(99, 102, 241, 0.03); }
        .issue.highlight { background: var(--accent-subtle); }
        .issue-top { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
        .sev-pill {
            font-size: 10px; font-weight: 700; text-transform: uppercase;
            letter-spacing: 0.04em; padding: 2px 8px; border-radius: 4px;
        }
        .sev-pill.critical { background: var(--critical-bg); color: var(--critical); border: 1px solid var(--critical-border); }
        .sev-pill.warning { background: var(--warning-bg); color: var(--warning); border: 1px solid var(--warning-border); }
        .sev-pill.info { background: var(--info-bg); color: var(--info); border: 1px solid var(--info-border); }
        .issue-type { font-weight: 600; font-size: 13px; }
        .issue-res {
            font-family: var(--font-mono); font-size: 12px;
            color: var(--text-secondary); background: var(--bg-card);
            padding: 1px 6px; border-radius: 3px; margin-left: auto;
        }
        .issue-msg { font-size: 13px; color: var(--text-secondary); line-height: 1.55; margin-bottom: 8px; }
        .issue-suggest {
            font-size: 12px; color: var(--accent-light); line-height: 1.5;
            display: flex; align-items: flex-start; gap: 6px;
            padding: 8px 10px; background: var(--accent-subtle);
            border-radius: var(--radius-sm); border: 1px solid rgba(99,102,241,0.15);
        }
        .issue-suggest svg { width: 14px; height: 14px; flex-shrink: 0; margin-top: 1px; }
        .issue-cmd {
            margin-top: 6px; display: flex; align-items: center; gap: 6px;
        }
        .issue-cmd code {
            font-family: var(--font-mono); font-size: 11px;
            background: var(--bg-card-elevated); color: var(--text-primary);
            padding: 4px 10px; border-radius: 4px; border: 1px solid var(--border);
            flex: 1; overflow-x: auto; white-space: nowrap;
        }
        .issue-cmd .copy-btn {
            padding: 4px 8px; font-size: 11px; cursor: pointer;
            background: var(--bg-card); border: 1px solid var(--border);
            border-radius: 4px; color: var(--text-secondary);
            transition: all 0.15s; flex-shrink: 0;
        }
        .issue-cmd .copy-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
        .no-issues {
            padding: 28px; text-align: center; color: var(--text-muted);
            display: flex; flex-direction: column; align-items: center; gap: 6px;
        }
        .no-issues svg { width: 28px; height: 28px; color: var(--success); }

        /* ========================================
           Modal
           ======================================== */
        .modal-overlay {
            position: fixed; inset: 0; background: rgba(0,0,0,0.5);
            display: flex; align-items: center; justify-content: center;
            z-index: 200; opacity: 0; visibility: hidden;
            transition: opacity 0.2s, visibility 0.2s;
            backdrop-filter: blur(4px);
        }
        .modal-overlay.active { opacity: 1; visibility: visible; }
        .modal-box {
            background: var(--bg-secondary); border-radius: var(--radius-lg);
            border: 1px solid var(--border); width: 90%; max-width: 520px;
            max-height: 85vh; overflow: auto;
            transform: scale(0.96); transition: transform 0.2s;
            box-shadow: var(--shadow-lg);
        }
        .modal-overlay.active .modal-box { transform: scale(1); }
        .modal-head {
            padding: 16px 20px; border-bottom: 1px solid var(--border);
            display: flex; justify-content: space-between; align-items: center;
        }
        .modal-head h2 { font-size: 16px; font-weight: 600; }
        .modal-close {
            background: none; border: none; color: var(--text-muted);
            cursor: pointer; padding: 4px; display: flex;
        }
        .modal-close:hover { color: var(--text-primary); }
        .modal-body { padding: 20px; }

        /* Shortcuts modal */
        .sc-list { display: flex; flex-direction: column; gap: 10px; }
        .sc-item { display: flex; justify-content: space-between; align-items: center; }
        .sc-keys { display: flex; gap: 4px; }
        .sc-keys kbd {
            background: var(--bg-card); padding: 3px 7px; border-radius: 4px;
            font-family: var(--font-mono); font-size: 11px;
            border: 1px solid var(--border);
        }
        .sc-desc { color: var(--text-secondary); font-size: 13px; }

        /* ========================================
           AI Settings Modal
           ======================================== */
        .ai-form { display: flex; flex-direction: column; gap: 16px; }
        .ai-field { display: flex; flex-direction: column; gap: 4px; }
        .ai-field label {
            font-size: 12px; font-weight: 500; color: var(--text-secondary);
            text-transform: uppercase; letter-spacing: 0.04em;
        }
        .ai-field input, .ai-field select {
            padding: 9px 12px; background: var(--bg-input);
            border: 1px solid var(--border); border-radius: var(--radius-sm);
            color: var(--text-primary); font-size: 13px;
        }
        .ai-field input:focus, .ai-field select:focus {
            outline: none; border-color: var(--accent);
        }
        .ai-toggle {
            display: flex; align-items: center; gap: 10px;
        }
        .toggle-switch {
            position: relative; width: 40px; height: 22px; cursor: pointer;
        }
        .toggle-switch input { opacity: 0; width: 0; height: 0; }
        .toggle-track {
            position: absolute; inset: 0; background: var(--bg-hover);
            border-radius: 11px; transition: background 0.2s;
        }
        .toggle-track::after {
            content: ''; position: absolute; width: 16px; height: 16px;
            left: 3px; top: 3px; background: white; border-radius: 50%;
            transition: transform 0.2s;
        }
        .toggle-switch input:checked + .toggle-track { background: var(--accent); }
        .toggle-switch input:checked + .toggle-track::after { transform: translateX(18px); }
        .ai-status {
            display: flex; align-items: center; gap: 8px;
            padding: 10px 14px; border-radius: var(--radius-sm);
            font-size: 12px; font-weight: 500;
        }
        .ai-status.ready { background: var(--success-bg); color: var(--success); border: 1px solid var(--success-border); }
        .ai-status.off { background: var(--bg-card); color: var(--text-muted); border: 1px solid var(--border); }
        .ai-status-dot {
            width: 6px; height: 6px; border-radius: 50%;
        }
        .ai-status.ready .ai-status-dot { background: var(--success); }
        .ai-status.off .ai-status-dot { background: var(--text-muted); }

        /* ========================================
           Toast
           ======================================== */
        .toast-wrap {
            position: fixed; bottom: 20px; right: 20px; z-index: 300;
            display: flex; flex-direction: column; gap: 8px;
        }
        .toast {
            padding: 10px 16px; border-radius: var(--radius-sm);
            font-size: 13px; font-weight: 500; box-shadow: var(--shadow-lg);
            animation: toast-in 0.3s ease forwards;
        }
        .toast.success { background: var(--success); color: white; }
        .toast.error { background: var(--critical); color: white; }
        .toast.removing { animation: toast-out 0.3s ease forwards; }

        /* ========================================
           Timestamp footer
           ======================================== */
        .footer-ts {
            text-align: center; padding: 20px; font-size: 11px;
            color: var(--text-muted);
        }

        /* ========================================
           Responsive
           ======================================== */
        @media (max-width: 1024px) {
            .stats-row { grid-template-columns: 1fr; }
            .score-card { order: -1; }
            .metric-cards { grid-template-columns: repeat(2, 1fr); }
        }
        @media (max-width: 768px) {
            .topbar { padding: 0 16px; }
            .main { padding: 16px; }
            .toolbar { flex-direction: column; }
            .search-wrap { width: 100%; }
            .toolbar-right { width: 100%; justify-content: flex-end; }
            .metric-cards { grid-template-columns: repeat(2, 1fr); }
            .checkers { grid-template-columns: 1fr; }
            .stats-row { grid-template-columns: 1fr; }
        }

        /* ========================================
           Scrollbar
           ======================================== */
        ::-webkit-scrollbar { width: 6px; height: 6px; }
        ::-webkit-scrollbar-track { background: transparent; }
        ::-webkit-scrollbar-thumb { background: var(--bg-hover); border-radius: 3px; }
        ::-webkit-scrollbar-thumb:hover { background: var(--text-muted); }
    </style>
</head>
<body>
    <!-- Topbar -->
    <header class="topbar">
        <div class="topbar-left">
            <div class="topbar-logo">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
            </div>
            <span class="topbar-title">kube-assist</span>
            <span class="topbar-badge ok" id="topBadge">Healthy</span>
        </div>
        <div class="topbar-right">
            <div class="topbar-status">
                <div class="topbar-dot" id="connDot"></div>
                <span id="connText">Connecting...</span>
            </div>
            <button class="btn btn-icon" id="aiBtn" title="AI Settings">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2a4 4 0 0 0-4 4c0 2 2 3 2 6H8a2 2 0 1 0 0 4h8a2 2 0 1 0 0-4h-2c0-3 2-4 2-6a4 4 0 0 0-4-4z"/><line x1="10" y1="20" x2="14" y2="20"/></svg>
            </button>
            <button class="btn btn-icon" id="pauseBtn" title="Pause updates (P)">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg>
            </button>
            <button class="btn btn-icon" id="themeBtn" title="Toggle theme (T)">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
            </button>
            <button class="btn btn-icon" id="helpBtn" title="Keyboard shortcuts (?)">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
            </button>
            <button class="btn btn-accent" onclick="triggerCheck()">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
                Refresh
            </button>
        </div>
    </header>

    <main class="main">
        <!-- Loading -->
        <div id="loading" class="loading-screen">
            <div class="loading-spinner"></div>
            <p>Connecting to health monitor...</p>
        </div>

        <!-- Skeletons -->
        <div id="skeletons" style="display:none;">
            <div class="stats-row">
                <div class="skeleton" style="height:170px;"></div>
                <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:12px;">
                    <div class="skeleton" style="height:90px;"></div>
                    <div class="skeleton" style="height:90px;"></div>
                    <div class="skeleton" style="height:90px;"></div>
                    <div class="skeleton" style="height:90px;"></div>
                </div>
            </div>
            <div class="checkers">
                <div class="skeleton" style="height:200px;"></div>
                <div class="skeleton" style="height:200px;"></div>
            </div>
        </div>

        <!-- Dashboard -->
        <div id="dashboard" style="display:none;">
            <!-- Toolbar -->
            <div class="toolbar">
                <div class="search-wrap">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
                    <input type="text" id="searchInput" placeholder="Search resources, types, messages... (/)" autocomplete="off">
                </div>
                <select class="filter-select" id="nsFilter"><option value="">All Namespaces</option></select>
                <select class="filter-select" id="chkFilter"><option value="">All Checkers</option></select>
                <div class="severity-tabs" id="sevTabs">
                    <button class="sev-tab active" data-severity="all">All <span class="cnt" id="cntAll">0</span></button>
                    <button class="sev-tab" data-severity="critical">Critical <span class="cnt" id="cntCrit">0</span></button>
                    <button class="sev-tab" data-severity="warning">Warning <span class="cnt" id="cntWarn">0</span></button>
                    <button class="sev-tab" data-severity="info">Info <span class="cnt" id="cntInfo">0</span></button>
                </div>
                <div class="toolbar-right">
                    <button class="btn btn-sm" onclick="exportJSON()" title="Export JSON">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                        JSON
                    </button>
                    <button class="btn btn-sm" onclick="exportCSV()" title="Export CSV">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                        CSV
                    </button>
                </div>
            </div>

            <!-- Stats -->
            <div class="stats-row">
                <div class="score-card">
                    <div class="ring-wrap">
                        <svg class="ring-svg" width="110" height="110">
                            <circle class="ring-bg" cx="55" cy="55" r="45"></circle>
                            <circle class="ring-fill" id="ringFill" cx="55" cy="55" r="45"></circle>
                        </svg>
                        <div class="ring-text">
                            <div class="ring-val" id="scoreVal">--</div>
                            <div class="ring-lbl">Health</div>
                        </div>
                    </div>
                    <div class="score-title">Overall Health Score</div>
                </div>
                <div class="metric-cards">
                    <div class="metric-card ok"><div class="metric-label">Healthy</div><div class="metric-val" id="mHealthy">0</div></div>
                    <div class="metric-card crit"><div class="metric-label">Critical</div><div class="metric-val" id="mCrit">0</div></div>
                    <div class="metric-card warn"><div class="metric-label">Warning</div><div class="metric-val" id="mWarn">0</div></div>
                    <div class="metric-card info-c"><div class="metric-label">Info</div><div class="metric-val" id="mInfo">0</div></div>
                </div>
            </div>

            <!-- Timeline -->
            <div class="timeline-row">
                <div class="timeline-title">Check History (Last 5)</div>
                <div class="timeline" id="timeline"></div>
            </div>

            <!-- Namespaces -->
            <div class="ns-row">
                <span class="ns-label">Monitoring:</span>
                <div id="nsList"></div>
            </div>

            <!-- Checkers -->
            <div class="checkers" id="checkersGrid"></div>

            <!-- Timestamp -->
            <div class="footer-ts" id="footerTs"></div>
        </div>
    </main>

    <!-- Help Modal -->
    <div class="modal-overlay" id="helpModal">
        <div class="modal-box">
            <div class="modal-head">
                <h2>Keyboard Shortcuts</h2>
                <button class="modal-close" onclick="toggleHelp()"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
            </div>
            <div class="modal-body">
                <div class="sc-list">
                    <div class="sc-item"><span class="sc-keys"><kbd>/</kbd></span><span class="sc-desc">Focus search</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>j</kbd></span><span class="sc-desc">Next issue</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>k</kbd></span><span class="sc-desc">Previous issue</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>f</kbd></span><span class="sc-desc">Focus namespace filter</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>t</kbd></span><span class="sc-desc">Toggle theme</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>p</kbd></span><span class="sc-desc">Pause/resume updates</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>r</kbd></span><span class="sc-desc">Refresh data</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>a</kbd></span><span class="sc-desc">AI Settings</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>1</kbd><kbd>2</kbd><kbd>3</kbd><kbd>4</kbd></span><span class="sc-desc">Filter by severity</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>?</kbd></span><span class="sc-desc">Toggle this help</span></div>
                    <div class="sc-item"><span class="sc-keys"><kbd>Esc</kbd></span><span class="sc-desc">Close modal / blur input</span></div>
                </div>
            </div>
        </div>
    </div>

    <!-- AI Settings Modal -->
    <div class="modal-overlay" id="aiModal">
        <div class="modal-box" style="max-width: 440px;">
            <div class="modal-head">
                <h2>AI Configuration</h2>
                <button class="modal-close" onclick="toggleAI()"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
            </div>
            <div class="modal-body">
                <div class="ai-form">
                    <div class="ai-status off" id="aiStatusBar">
                        <div class="ai-status-dot"></div>
                        <span id="aiStatusText">AI not configured</span>
                    </div>
                    <div class="ai-field">
                        <label>Enable AI Analysis</label>
                        <div class="ai-toggle">
                            <label class="toggle-switch">
                                <input type="checkbox" id="aiEnabledToggle">
                                <span class="toggle-track"></span>
                            </label>
                            <span style="font-size:13px;color:var(--text-secondary);" id="aiEnabledLabel">Disabled</span>
                        </div>
                    </div>
                    <div class="ai-field">
                        <label>Provider</label>
                        <select id="aiProvider">
                            <option value="anthropic">Anthropic (Claude)</option>
                            <option value="openai">OpenAI (GPT)</option>
                            <option value="noop">None (built-in only)</option>
                        </select>
                    </div>
                    <div class="ai-field">
                        <label>API Key</label>
                        <input type="password" id="aiApiKey" placeholder="sk-... or leave blank to keep current">
                    </div>
                    <div class="ai-field">
                        <label>Model (optional)</label>
                        <input type="text" id="aiModel" placeholder="e.g. claude-sonnet-4-5-20250929, gpt-4o">
                    </div>
                    <button class="btn btn-accent" style="width:100%;justify-content:center;" onclick="saveAISettings()">
                        Save Settings
                    </button>
                </div>
            </div>
        </div>
    </div>

    <!-- Toast container -->
    <div class="toast-wrap" id="toastWrap"></div>

    <script>
        // ========================================
        // XSS Prevention
        // ========================================
        function escapeHTML(str) {
            if (typeof str !== 'string') return str;
            return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
        }

        // ========================================
        // State
        // ========================================
        const S = {
            data: null,
            history: [],
            isPaused: false,
            theme: localStorage.getItem('kubeassist-theme') || 'dark',
            filters: { namespace: '', checker: '', severity: 'all', search: '' },
            collapsed: new Set(JSON.parse(localStorage.getItem('kubeassist-collapsed') || '[]')),
            issueIdx: -1,
            aiSettings: null
        };
        let evtSrc = null;

        // ========================================
        // Init
        // ========================================
        function init() {
            applyTheme();
            initKeys();
            initListeners();
            loadAISettings();
            connect();
        }

        function initListeners() {
            document.getElementById('searchInput').addEventListener('input', e => {
                S.filters.search = e.target.value.toLowerCase();
                render();
            });
            document.getElementById('nsFilter').addEventListener('change', e => {
                S.filters.namespace = e.target.value;
                render();
            });
            document.getElementById('chkFilter').addEventListener('change', e => {
                S.filters.checker = e.target.value;
                render();
            });
            document.querySelectorAll('.sev-tab').forEach(tab => {
                tab.addEventListener('click', () => {
                    document.querySelectorAll('.sev-tab').forEach(t => t.classList.remove('active'));
                    tab.classList.add('active');
                    S.filters.severity = tab.dataset.severity;
                    render();
                });
            });
            document.getElementById('themeBtn').addEventListener('click', toggleTheme);
            document.getElementById('pauseBtn').addEventListener('click', togglePause);
            document.getElementById('helpBtn').addEventListener('click', toggleHelp);
            document.getElementById('aiBtn').addEventListener('click', toggleAI);
            document.getElementById('helpModal').addEventListener('click', e => { if (e.target.id === 'helpModal') toggleHelp(); });
            document.getElementById('aiModal').addEventListener('click', e => { if (e.target.id === 'aiModal') toggleAI(); });
            document.getElementById('aiEnabledToggle').addEventListener('change', e => {
                document.getElementById('aiEnabledLabel').textContent = e.target.checked ? 'Enabled' : 'Disabled';
            });
        }

        // ========================================
        // Keyboard Shortcuts
        // ========================================
        function initKeys() {
            document.addEventListener('keydown', e => {
                if (e.target.tagName === 'INPUT' || e.target.tagName === 'SELECT' || e.target.tagName === 'TEXTAREA') {
                    if (e.key === 'Escape') e.target.blur();
                    return;
                }
                switch (e.key) {
                    case '/': e.preventDefault(); document.getElementById('searchInput').focus(); break;
                    case 'j': navIssue(1); break;
                    case 'k': navIssue(-1); break;
                    case 'f': document.getElementById('nsFilter').focus(); break;
                    case 't': toggleTheme(); break;
                    case 'p': togglePause(); break;
                    case 'r': triggerCheck(); break;
                    case 'a': toggleAI(); break;
                    case '?': toggleHelp(); break;
                    case '1': setSevTab('all'); break;
                    case '2': setSevTab('critical'); break;
                    case '3': setSevTab('warning'); break;
                    case '4': setSevTab('info'); break;
                    case 'Escape':
                        if (document.getElementById('helpModal').classList.contains('active')) toggleHelp();
                        else if (document.getElementById('aiModal').classList.contains('active')) toggleAI();
                        break;
                }
            });
        }

        function setSevTab(sev) {
            document.querySelectorAll('.sev-tab').forEach(t => t.classList.remove('active'));
            document.querySelector('.sev-tab[data-severity="' + sev + '"]').classList.add('active');
            S.filters.severity = sev;
            render();
        }

        function navIssue(dir) {
            const items = document.querySelectorAll('.issue');
            if (!items.length) return;
            items.forEach(i => i.classList.remove('highlight'));
            S.issueIdx += dir;
            if (S.issueIdx < 0) S.issueIdx = items.length - 1;
            if (S.issueIdx >= items.length) S.issueIdx = 0;
            items[S.issueIdx].classList.add('highlight');
            items[S.issueIdx].scrollIntoView({ behavior: 'smooth', block: 'center' });
        }

        // ========================================
        // Theme
        // ========================================
        function applyTheme() {
            document.documentElement.setAttribute('data-theme', S.theme);
            updateThemeIcon();
        }

        function toggleTheme() {
            S.theme = S.theme === 'dark' ? 'light' : 'dark';
            localStorage.setItem('kubeassist-theme', S.theme);
            applyTheme();
        }

        function updateThemeIcon() {
            const btn = document.getElementById('themeBtn');
            btn.innerHTML = S.theme === 'light'
                ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>'
                : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>';
        }

        // ========================================
        // Pause
        // ========================================
        function togglePause() {
            S.isPaused = !S.isPaused;
            const btn = document.getElementById('pauseBtn');
            const dot = document.getElementById('connDot');
            const txt = document.getElementById('connText');
            if (S.isPaused) {
                btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>';
                btn.title = 'Resume updates (P)';
                dot.classList.add('paused');
                txt.textContent = 'Paused';
                if (evtSrc) { evtSrc.close(); evtSrc = null; }
            } else {
                btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg>';
                btn.title = 'Pause updates (P)';
                dot.classList.remove('paused');
                connect();
            }
        }

        // ========================================
        // Modals
        // ========================================
        function toggleHelp() { document.getElementById('helpModal').classList.toggle('active'); }
        function toggleAI() { document.getElementById('aiModal').classList.toggle('active'); }

        // ========================================
        // SSE Connection
        // ========================================
        async function fetchInitial() {
            try {
                const r = await fetch('/api/health');
                const d = await r.json();
                if (d && d.results) updateData(d);
                else showSkel();
            } catch(e) { showSkel(); }
        }

        function showSkel() {
            document.getElementById('loading').style.display = 'none';
            document.getElementById('skeletons').style.display = 'block';
        }

        function connect() {
            if (S.isPaused) return;
            fetchInitial();
            evtSrc = new EventSource('/api/events');
            evtSrc.onopen = () => {
                document.getElementById('connDot').classList.remove('off','paused');
                document.getElementById('connText').textContent = 'Connected';
            };
            evtSrc.onmessage = e => {
                if (S.isPaused) return;
                updateData(JSON.parse(e.data));
            };
            evtSrc.onerror = () => {
                document.getElementById('connDot').classList.add('off');
                document.getElementById('connText').textContent = 'Disconnected';
                if (!S.isPaused) setTimeout(connect, 5000);
            };
        }

        // ========================================
        // Data
        // ========================================
        function updateData(d) {
            S.data = d;
            addHistory(d);
            populateFilters(d);
            render();
        }

        function addHistory(d) {
            S.history.push({
                ts: new Date(d.timestamp),
                c: d.summary.criticalCount,
                w: d.summary.warningCount,
                i: d.summary.infoCount,
                h: d.summary.totalHealthy
            });
            if (S.history.length > 5) S.history.shift();
        }

        function populateFilters(d) {
            const ns = document.getElementById('nsFilter');
            const cv = ns.value;
            ns.innerHTML = '<option value="">All Namespaces</option>';
            (d.namespaces||[]).forEach(n => {
                const o = document.createElement('option'); o.value = n; o.textContent = n; ns.appendChild(o);
            });
            ns.value = cv;

            const cs = document.getElementById('chkFilter');
            const cc = cs.value;
            cs.innerHTML = '<option value="">All Checkers</option>';
            Object.keys(d.results||{}).forEach(k => {
                const o = document.createElement('option'); o.value = k;
                o.textContent = k.charAt(0).toUpperCase() + k.slice(1);
                cs.appendChild(o);
            });
            cs.value = cc;
        }

        // ========================================
        // AI Settings
        // ========================================
        async function loadAISettings() {
            try {
                const r = await fetch('/api/settings/ai');
                const d = await r.json();
                S.aiSettings = d;
                applyAIUI(d);
            } catch(e) { /* settings endpoint may not be ready yet */ }
        }

        function applyAIUI(d) {
            document.getElementById('aiEnabledToggle').checked = d.enabled;
            document.getElementById('aiEnabledLabel').textContent = d.enabled ? 'Enabled' : 'Disabled';
            if (d.provider) document.getElementById('aiProvider').value = d.provider;
            if (d.model) document.getElementById('aiModel').value = d.model;
            document.getElementById('aiApiKey').value = '';
            const bar = document.getElementById('aiStatusBar');
            const txt = document.getElementById('aiStatusText');
            if (d.enabled && d.providerReady && d.provider !== 'noop') {
                bar.className = 'ai-status ready';
                txt.textContent = 'AI active (' + escapeHTML(d.provider) + (d.model ? ' / ' + escapeHTML(d.model) : '') + ')';
            } else {
                bar.className = 'ai-status off';
                txt.textContent = d.enabled ? 'AI enabled but provider not ready' : 'AI not configured';
            }
        }

        async function saveAISettings() {
            const body = {
                enabled: document.getElementById('aiEnabledToggle').checked,
                provider: document.getElementById('aiProvider').value,
                model: document.getElementById('aiModel').value || ''
            };
            const key = document.getElementById('aiApiKey').value;
            if (key) body.apiKey = key;
            try {
                const r = await fetch('/api/settings/ai', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(body)
                });
                if (!r.ok) {
                    const t = await r.text();
                    toast('Failed: ' + t, 'error');
                    return;
                }
                const d = await r.json();
                S.aiSettings = d;
                applyAIUI(d);
                toast('AI settings saved', 'success');
            } catch(e) {
                toast('Network error', 'error');
            }
        }

        // ========================================
        // Toast
        // ========================================
        function toast(msg, type) {
            const wrap = document.getElementById('toastWrap');
            const el = document.createElement('div');
            el.className = 'toast ' + type;
            el.textContent = msg;
            wrap.appendChild(el);
            setTimeout(() => {
                el.classList.add('removing');
                setTimeout(() => el.remove(), 300);
            }, 3000);
        }

        // ========================================
        // Render
        // ========================================
        function render() {
            if (!S.data) return;
            document.getElementById('loading').style.display = 'none';
            document.getElementById('skeletons').style.display = 'none';
            document.getElementById('dashboard').style.display = 'block';

            const d = S.data;
            const filtered = applyFilters(d);

            // Metrics
            document.getElementById('mHealthy').textContent = d.summary.totalHealthy;
            document.getElementById('mCrit').textContent = d.summary.criticalCount;
            document.getElementById('mWarn').textContent = d.summary.warningCount;
            document.getElementById('mInfo').textContent = d.summary.infoCount;

            // Tab counts
            const total = d.summary.criticalCount + d.summary.warningCount + d.summary.infoCount;
            document.getElementById('cntAll').textContent = total;
            document.getElementById('cntCrit').textContent = d.summary.criticalCount;
            document.getElementById('cntWarn').textContent = d.summary.warningCount;
            document.getElementById('cntInfo').textContent = d.summary.infoCount;

            // Top badge
            updateBadge(total, d.summary.criticalCount);

            // Score
            updateScore(d);

            // Timeline
            renderTimeline();

            // Namespaces
            renderNS(d.namespaces || []);

            // Checkers
            renderCheckers(filtered);

            // Footer
            document.getElementById('footerTs').textContent = 'Last updated: ' + new Date(d.timestamp).toLocaleTimeString();
        }

        function applyFilters(d) {
            const out = {};
            const { namespace, checker, severity, search } = S.filters;
            for (const [name, result] of Object.entries(d.results || {})) {
                if (checker && name !== checker) continue;
                const issues = (result.issues || []).filter(iss => {
                    if (namespace && iss.namespace !== namespace) return false;
                    if (severity !== 'all' && iss.severity.toLowerCase() !== severity) return false;
                    if (search) {
                        const s = (iss.resource + ' ' + iss.namespace + ' ' + iss.message + ' ' + iss.type).toLowerCase();
                        if (!s.includes(search)) return false;
                    }
                    return true;
                });
                out[name] = { ...result, issues };
            }
            return out;
        }

        function updateBadge(total, crit) {
            const b = document.getElementById('topBadge');
            if (total === 0) {
                b.className = 'topbar-badge ok';
                b.textContent = 'Healthy';
            } else if (crit > 0) {
                b.className = 'topbar-badge crit';
                b.textContent = crit + ' critical';
            } else {
                b.className = 'topbar-badge warn';
                b.textContent = total + ' issue' + (total !== 1 ? 's' : '');
            }
        }

        function updateScore(d) {
            const { totalHealthy, criticalCount, warningCount, infoCount } = d.summary;
            const tot = totalHealthy + criticalCount + warningCount + infoCount;
            if (tot === 0) { document.getElementById('scoreVal').textContent = '--'; return; }
            const w = (criticalCount*3)+(warningCount*2)+(infoCount*1);
            const score = Math.max(0, Math.round(100*(1-w/(tot*3))));
            document.getElementById('scoreVal').textContent = score;
            const ring = document.getElementById('ringFill');
            const circ = 2*Math.PI*45;
            ring.style.strokeDashoffset = circ - (score/100)*circ;
            ring.classList.remove('warn','crit');
            if (score < 50) ring.classList.add('crit');
            else if (score < 80) ring.classList.add('warn');
        }

        function renderTimeline() {
            const c = document.getElementById('timeline');
            c.innerHTML = '';
            const maxH = 40;
            const entries = [...S.history];
            while (entries.length < 5) entries.unshift(null);
            entries.forEach(e => {
                const bar = document.createElement('div');
                bar.className = 'tl-bar';
                if (!e) {
                    bar.innerHTML = '<div class="tl-fill" style="height:' + maxH + 'px;background:var(--bg-card);border-radius:3px;"></div><div class="tl-time">--</div>';
                } else {
                    const t = e.c+e.w+e.i+e.h;
                    const ch = t>0?(e.c/t)*maxH:0, wh=t>0?(e.w/t)*maxH:0, ih=t>0?(e.i/t)*maxH:0;
                    const hh = maxH-ch-wh-ih;
                    let segs = '';
                    if (hh>0) segs += '<div class="tl-seg h" style="height:'+hh+'px"></div>';
                    if (ih>0) segs += '<div class="tl-seg i" style="height:'+ih+'px"></div>';
                    if (wh>0) segs += '<div class="tl-seg w" style="height:'+wh+'px"></div>';
                    if (ch>0) segs += '<div class="tl-seg c" style="height:'+ch+'px"></div>';
                    bar.innerHTML = '<div class="tl-fill" style="height:'+maxH+'px;">'+segs+'</div><div class="tl-time">'+e.ts.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'})+'</div>';
                }
                c.appendChild(bar);
            });
        }

        function renderNS(nss) {
            const c = document.getElementById('nsList');
            c.innerHTML = nss.length > 0
                ? nss.map(n => '<span class="ns-tag">' + escapeHTML(n) + '</span>').join('')
                : '<span class="ns-tag">none</span>';
        }

        function renderCheckers(results) {
            const grid = document.getElementById('checkersGrid');
            grid.innerHTML = '';
            const sorted = Object.entries(results).sort((a,b) => (b[1].issues||[]).length - (a[1].issues||[]).length);
            for (const [name, result] of sorted) {
                grid.appendChild(createChecker(name, result));
            }
        }

        function createChecker(name, result) {
            const card = document.createElement('div');
            card.className = 'chk-card';
            if (S.collapsed.has(name)) card.classList.add('collapsed');
            const issues = result.issues || [];
            const cc = issues.filter(i => i.severity.toLowerCase()==='critical').length;
            const wc = issues.filter(i => i.severity.toLowerCase()==='warning').length;
            const ic = issues.filter(i => i.severity.toLowerCase()==='info').length;
            if (cc > 0) card.classList.add('has-crit');
            const icon = getIcon(name);
            card.innerHTML =
                '<div class="chk-head" onclick="toggleChk(\'' + escapeHTML(name) + '\')">' +
                    '<div class="chk-head-left">' +
                        '<div class="chk-icon">' + icon + '</div>' +
                        '<span class="chk-name">' + escapeHTML(name) + '</span>' +
                    '</div>' +
                    '<div class="chk-head-right">' +
                        '<span class="chk-stat ok">' + result.healthy + ' ok</span>' +
                        (cc > 0 ? '<span class="chk-stat c">' + cc + ' crit</span>' : '') +
                        (wc > 0 ? '<span class="chk-stat w">' + wc + ' warn</span>' : '') +
                        (ic > 0 ? '<span class="chk-stat i">' + ic + ' info</span>' : '') +
                        '<svg class="chk-chevron" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 9l6 6 6-6"/></svg>' +
                    '</div>' +
                '</div>' +
                '<div class="chk-body">' +
                    (issues.length === 0
                        ? '<div class="no-issues"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>All resources healthy</div>'
                        : issues.map(i => issueHTML(i)).join(''))
                + '</div>';
            return card;
        }

        function getIcon(name) {
            const icons = {
                workloads: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>',
                secrets: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>',
                pvcs: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>',
                quotas: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg>',
                networkpolicies: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>',
                helmreleases: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M16 8l-8 8M8 8l8 8"/></svg>',
                kustomizations: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>',
                gitrepositories: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="4"/><line x1="1.05" y1="12" x2="7" y2="12"/><line x1="17.01" y1="12" x2="22.96" y2="12"/></svg>'
            };
            return icons[name] || '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/></svg>';
        }

        // ========================================
        // Issue Rendering
        // ========================================
        function cleanSuggestion(s) {
            if (!s) return '';
            // Remove NoOp AI prefix for cleaner display
            return s.replace(/^\[NoOp AI\]\s*/i, '').replace(/\n\n?AI Analysis:\s*\[NoOp AI\]\s*/i, '');
        }

        function extractCommand(issue) {
            const t = issue.type.toLowerCase();
            const res = issue.resource;
            const ns = issue.namespace;
            if (t.includes('crashloop') || t.includes('oomkilled') || t.includes('error')) {
                if (res.startsWith('deployment/')) return 'kubectl rollout restart ' + res + ' -n ' + ns;
                return 'kubectl logs ' + res + ' -n ' + ns + ' --tail=50';
            }
            if (t.includes('imagepull')) return 'kubectl describe ' + res + ' -n ' + ns;
            if (t.includes('pending') || t.includes('lost')) return 'kubectl describe ' + res + ' -n ' + ns;
            if (t.includes('expired') || t.includes('expiring')) return 'kubectl get secret ' + res + ' -n ' + ns + ' -o yaml';
            if (t.includes('quota')) return 'kubectl describe resourcequota -n ' + ns;
            if (t.includes('missing') && t.includes('limit')) return 'kubectl get ' + res + ' -n ' + ns + ' -o yaml | grep -A5 resources';
            if (t.includes('missing') && t.includes('probe')) return 'kubectl get ' + res + ' -n ' + ns + ' -o yaml | grep -A3 Probe';
            if (t.includes('upgrade') || t.includes('helmrelease') || t.includes('stale')) return 'flux reconcile helmrelease ' + res.replace(/^helmrelease\//,'') + ' -n ' + ns;
            if (t.includes('kustomization')) return 'flux reconcile kustomization ' + res.replace(/^kustomization\//,'') + ' -n ' + ns;
            if (t.includes('gitrepository') || t.includes('clone') || t.includes('auth')) return 'flux reconcile source git ' + res.replace(/^gitrepository\//,'') + ' -n ' + ns;
            if (t.includes('networkpolic') || t.includes('missing polic') || t.includes('permissive')) return 'kubectl get networkpolicy -n ' + ns;
            return 'kubectl describe ' + res + ' -n ' + ns;
        }

        function issueHTML(issue) {
            const sev = issue.severity.toLowerCase();
            const suggestion = cleanSuggestion(issue.suggestion);
            const cmd = extractCommand(issue);
            const cmdId = 'cmd-' + Math.random().toString(36).substr(2,8);
            return '<div class="issue">' +
                '<div class="issue-top">' +
                    '<span class="sev-pill ' + sev + '">' + escapeHTML(issue.severity) + '</span>' +
                    '<span class="issue-type">' + escapeHTML(issue.type) + '</span>' +
                    '<span class="issue-res">' + escapeHTML(issue.namespace) + '/' + escapeHTML(issue.resource) + '</span>' +
                '</div>' +
                '<div class="issue-msg">' + escapeHTML(issue.message) + '</div>' +
                (suggestion ? '<div class="issue-suggest"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg><span>' + escapeHTML(suggestion) + '</span></div>' : '') +
                '<div class="issue-cmd">' +
                    '<code id="' + cmdId + '">' + escapeHTML(cmd) + '</code>' +
                    '<button class="copy-btn" onclick="copyCmd(\'' + cmdId + '\')">Copy</button>' +
                '</div>' +
            '</div>';
        }

        function copyCmd(id) {
            const el = document.getElementById(id);
            if (!el) return;
            navigator.clipboard.writeText(el.textContent).then(() => {
                toast('Copied to clipboard', 'success');
            }).catch(() => {
                // Fallback
                const range = document.createRange();
                range.selectNodeContents(el);
                const sel = window.getSelection();
                sel.removeAllRanges();
                sel.addRange(range);
                document.execCommand('copy');
                sel.removeAllRanges();
                toast('Copied to clipboard', 'success');
            });
        }

        function toggleChk(name) {
            if (S.collapsed.has(name)) S.collapsed.delete(name);
            else S.collapsed.add(name);
            localStorage.setItem('kubeassist-collapsed', JSON.stringify([...S.collapsed]));
            render();
        }

        // ========================================
        // Export
        // ========================================
        function exportJSON() {
            if (!S.data) return;
            const b = new Blob([JSON.stringify(S.data, null, 2)], { type: 'application/json' });
            const u = URL.createObjectURL(b);
            const a = document.createElement('a');
            a.href = u;
            a.download = 'health-report-' + new Date().toISOString().split('T')[0] + '.json';
            a.click();
            URL.revokeObjectURL(u);
        }

        function exportCSV() {
            if (!S.data) return;
            const rows = [['Checker','Severity','Type','Namespace','Resource','Message','Suggestion']];
            for (const [chk, res] of Object.entries(S.data.results || {})) {
                for (const iss of (res.issues || [])) {
                    rows.push([chk, iss.severity, iss.type, iss.namespace, iss.resource,
                        '"' + (iss.message||'').replace(/"/g,'""') + '"',
                        '"' + (iss.suggestion||'').replace(/"/g,'""') + '"']);
                }
            }
            const csv = rows.map(r => r.join(',')).join('\n');
            const b = new Blob([csv], { type: 'text/csv' });
            const u = URL.createObjectURL(b);
            const a = document.createElement('a');
            a.href = u;
            a.download = 'health-report-' + new Date().toISOString().split('T')[0] + '.csv';
            a.click();
            URL.revokeObjectURL(u);
        }

        // ========================================
        // API
        // ========================================
        async function triggerCheck() {
            try { await fetch('/api/check', { method: 'POST' }); }
            catch(e) { console.error('Failed to trigger check:', e); }
        }

        // ========================================
        // Go
        // ========================================
        init();
    </script>
</body>
</html>
`
