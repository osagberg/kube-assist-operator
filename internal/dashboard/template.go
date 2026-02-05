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
    <title>Team Health Dashboard - kube-assist</title>
    <style>
        /* ========================================
           CSS Variables - Dark Theme (Default)
           ======================================== */
        :root {
            --bg-primary: #0f172a;
            --bg-secondary: #1e293b;
            --bg-card: #334155;
            --bg-hover: #475569;
            --text-primary: #f8fafc;
            --text-secondary: #94a3b8;
            --text-muted: #64748b;
            --accent-blue: #3b82f6;
            --accent-purple: #8b5cf6;
            --critical: #ef4444;
            --critical-bg: rgba(239, 68, 68, 0.15);
            --warning: #f59e0b;
            --warning-bg: rgba(245, 158, 11, 0.15);
            --info: #3b82f6;
            --info-bg: rgba(59, 130, 246, 0.15);
            --success: #22c55e;
            --success-bg: rgba(34, 197, 94, 0.15);
            --border-color: rgba(255, 255, 255, 0.1);
            --glass-bg: rgba(30, 41, 59, 0.7);
            --glass-border: rgba(255, 255, 255, 0.1);
            --gradient-start: #3b82f6;
            --gradient-end: #8b5cf6;
            --shadow-color: rgba(0, 0, 0, 0.3);
            --skeleton-base: #334155;
            --skeleton-shine: #475569;
        }

        /* ========================================
           Light Theme
           ======================================== */
        [data-theme="light"] {
            --bg-primary: #f1f5f9;
            --bg-secondary: #ffffff;
            --bg-card: #e2e8f0;
            --bg-hover: #cbd5e1;
            --text-primary: #0f172a;
            --text-secondary: #475569;
            --text-muted: #94a3b8;
            --border-color: rgba(0, 0, 0, 0.1);
            --glass-bg: rgba(255, 255, 255, 0.7);
            --glass-border: rgba(0, 0, 0, 0.1);
            --shadow-color: rgba(0, 0, 0, 0.1);
            --skeleton-base: #e2e8f0;
            --skeleton-shine: #f1f5f9;
        }

        /* ========================================
           Base Styles & Reset
           ======================================== */
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
            transition: background-color 0.3s ease, color 0.3s ease;
        }

        /* ========================================
           Utility Classes
           ======================================== */
        .glass {
            background: var(--glass-bg);
            backdrop-filter: blur(12px);
            -webkit-backdrop-filter: blur(12px);
            border: 1px solid var(--glass-border);
        }

        .gradient-border {
            position: relative;
        }

        .gradient-border::before {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            height: 3px;
            background: linear-gradient(90deg, var(--gradient-start), var(--gradient-end));
            border-radius: 12px 12px 0 0;
        }

        .transition-all {
            transition: all 0.2s ease;
        }

        .hover-lift:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 25px var(--shadow-color);
        }

        /* ========================================
           Animations
           ======================================== */
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }

        @keyframes spin {
            to { transform: rotate(360deg); }
        }

        @keyframes critical-pulse {
            0%, 100% { box-shadow: 0 0 0 0 rgba(239, 68, 68, 0.4); }
            50% { box-shadow: 0 0 0 8px rgba(239, 68, 68, 0); }
        }

        @keyframes skeleton-shimmer {
            0% { background-position: 200% 0; }
            100% { background-position: -200% 0; }
        }

        @keyframes fade-in {
            from { opacity: 0; transform: translateY(10px); }
            to { opacity: 1; transform: translateY(0); }
        }

        @keyframes slide-down {
            from { opacity: 0; max-height: 0; }
            to { opacity: 1; max-height: 500px; }
        }

        @keyframes progress-ring {
            0% { stroke-dashoffset: 283; }
        }

        .animate-fade-in {
            animation: fade-in 0.3s ease forwards;
        }

        /* ========================================
           Header
           ======================================== */
        .header {
            background: linear-gradient(135deg, var(--bg-secondary) 0%, var(--bg-card) 100%);
            padding: 1rem 2rem;
            display: flex;
            justify-content: space-between;
            align-items: center;
            border-bottom: 1px solid var(--border-color);
            position: sticky;
            top: 0;
            z-index: 100;
            backdrop-filter: blur(10px);
        }

        .header-left {
            display: flex;
            align-items: center;
            gap: 1rem;
        }

        .header h1 {
            font-size: 1.5rem;
            font-weight: 600;
            background: linear-gradient(135deg, var(--text-primary), var(--text-secondary));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
        }

        .issue-badge {
            background: var(--critical);
            color: white;
            padding: 0.25rem 0.75rem;
            border-radius: 20px;
            font-size: 0.75rem;
            font-weight: 600;
            animation: critical-pulse 2s infinite;
        }

        .issue-badge.warning-badge {
            background: var(--warning);
            animation: none;
        }

        .issue-badge.healthy-badge {
            background: var(--success);
            animation: none;
        }

        .header-controls {
            display: flex;
            align-items: center;
            gap: 0.75rem;
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

        .status-dot.disconnected {
            background: var(--critical);
        }

        .status-dot.paused {
            background: var(--warning);
            animation: none;
        }

        /* ========================================
           Buttons
           ======================================== */
        .btn {
            background: var(--bg-card);
            color: var(--text-primary);
            border: 1px solid var(--border-color);
            padding: 0.5rem 1rem;
            border-radius: 8px;
            cursor: pointer;
            font-size: 0.875rem;
            display: flex;
            align-items: center;
            gap: 0.5rem;
            transition: all 0.2s ease;
        }

        .btn:hover {
            background: var(--bg-hover);
            transform: translateY(-1px);
        }

        .btn:active {
            transform: translateY(0);
        }

        .btn-primary {
            background: linear-gradient(135deg, var(--gradient-start), var(--gradient-end));
            border: none;
            color: white;
        }

        .btn-primary:hover {
            opacity: 0.9;
            background: linear-gradient(135deg, var(--gradient-start), var(--gradient-end));
        }

        .btn-icon {
            padding: 0.5rem;
            border-radius: 8px;
        }

        .btn-icon svg {
            width: 18px;
            height: 18px;
        }

        /* ========================================
           Container
           ======================================== */
        .container {
            max-width: 1600px;
            margin: 0 auto;
            padding: 1.5rem 2rem;
        }

        /* ========================================
           Toolbar
           ======================================== */
        .toolbar {
            display: flex;
            flex-wrap: wrap;
            gap: 1rem;
            margin-bottom: 1.5rem;
            padding: 1rem;
            background: var(--bg-secondary);
            border-radius: 12px;
            border: 1px solid var(--border-color);
            align-items: center;
        }

        .search-box {
            flex: 1;
            min-width: 200px;
            position: relative;
        }

        .search-box input {
            width: 100%;
            padding: 0.625rem 1rem 0.625rem 2.5rem;
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            color: var(--text-primary);
            font-size: 0.875rem;
            transition: all 0.2s ease;
        }

        .search-box input:focus {
            outline: none;
            border-color: var(--accent-blue);
            box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.2);
        }

        .search-box input::placeholder {
            color: var(--text-muted);
        }

        .search-box svg {
            position: absolute;
            left: 0.75rem;
            top: 50%;
            transform: translateY(-50%);
            width: 16px;
            height: 16px;
            color: var(--text-muted);
        }

        .filter-group {
            display: flex;
            gap: 0.5rem;
        }

        .filter-select {
            padding: 0.625rem 2rem 0.625rem 0.75rem;
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            color: var(--text-primary);
            font-size: 0.875rem;
            cursor: pointer;
            appearance: none;
            background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%2394a3b8' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpolyline points='6 9 12 15 18 9'%3E%3C/polyline%3E%3C/svg%3E");
            background-repeat: no-repeat;
            background-position: right 0.75rem center;
            transition: all 0.2s ease;
        }

        .filter-select:focus {
            outline: none;
            border-color: var(--accent-blue);
        }

        /* ========================================
           Severity Tabs
           ======================================== */
        .severity-tabs {
            display: flex;
            gap: 0.25rem;
            padding: 0.25rem;
            background: var(--bg-card);
            border-radius: 8px;
        }

        .severity-tab {
            padding: 0.5rem 1rem;
            border: none;
            background: transparent;
            color: var(--text-secondary);
            font-size: 0.875rem;
            border-radius: 6px;
            cursor: pointer;
            transition: all 0.2s ease;
            display: flex;
            align-items: center;
            gap: 0.375rem;
        }

        .severity-tab:hover {
            background: var(--bg-hover);
        }

        .severity-tab.active {
            background: var(--bg-secondary);
            color: var(--text-primary);
            font-weight: 500;
        }

        .severity-tab .count {
            font-size: 0.75rem;
            padding: 0.125rem 0.375rem;
            border-radius: 10px;
            background: var(--bg-hover);
        }

        .severity-tab.active .count {
            background: var(--accent-blue);
            color: white;
        }

        .severity-tab[data-severity="critical"].active .count { background: var(--critical); }
        .severity-tab[data-severity="warning"].active .count { background: var(--warning); }
        .severity-tab[data-severity="info"].active .count { background: var(--info); }

        .export-group {
            display: flex;
            gap: 0.5rem;
            margin-left: auto;
        }

        /* ========================================
           Summary Cards with Progress Ring
           ======================================== */
        .summary-section {
            display: grid;
            grid-template-columns: auto 1fr;
            gap: 1.5rem;
            margin-bottom: 1.5rem;
        }

        .health-score-card {
            background: var(--bg-secondary);
            border-radius: 16px;
            padding: 1.5rem 2rem;
            border: 1px solid var(--border-color);
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            gap: 0.75rem;
            min-width: 180px;
        }

        .progress-ring-container {
            position: relative;
            width: 120px;
            height: 120px;
        }

        .progress-ring {
            transform: rotate(-90deg);
        }

        .progress-ring-bg {
            fill: none;
            stroke: var(--bg-card);
            stroke-width: 8;
        }

        .progress-ring-fill {
            fill: none;
            stroke: var(--success);
            stroke-width: 8;
            stroke-linecap: round;
            stroke-dasharray: 283;
            stroke-dashoffset: 283;
            transition: stroke-dashoffset 1s ease, stroke 0.3s ease;
        }

        .progress-ring-fill.warning { stroke: var(--warning); }
        .progress-ring-fill.critical { stroke: var(--critical); }

        .progress-ring-text {
            position: absolute;
            top: 50%;
            left: 50%;
            transform: translate(-50%, -50%);
            text-align: center;
        }

        .progress-ring-value {
            font-size: 2rem;
            font-weight: 700;
            line-height: 1;
        }

        .progress-ring-label {
            font-size: 0.75rem;
            color: var(--text-secondary);
            margin-top: 0.25rem;
        }

        .health-score-title {
            font-size: 0.875rem;
            color: var(--text-secondary);
            font-weight: 500;
        }

        .summary-cards {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
            gap: 1rem;
        }

        .summary-card {
            background: var(--bg-secondary);
            border-radius: 12px;
            padding: 1.25rem;
            border: 1px solid var(--border-color);
            transition: all 0.2s ease;
        }

        .summary-card:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 20px var(--shadow-color);
        }

        .summary-card .label {
            font-size: 0.75rem;
            color: var(--text-secondary);
            margin-bottom: 0.5rem;
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }

        .summary-card .value {
            font-size: 2rem;
            font-weight: 700;
        }

        .summary-card.critical { border-left: 3px solid var(--critical); }
        .summary-card.critical .value { color: var(--critical); }
        .summary-card.warning { border-left: 3px solid var(--warning); }
        .summary-card.warning .value { color: var(--warning); }
        .summary-card.info { border-left: 3px solid var(--info); }
        .summary-card.info .value { color: var(--info); }
        .summary-card.healthy { border-left: 3px solid var(--success); }
        .summary-card.healthy .value { color: var(--success); }

        /* ========================================
           Timeline
           ======================================== */
        .timeline-section {
            margin-bottom: 1.5rem;
            padding: 1rem;
            background: var(--bg-secondary);
            border-radius: 12px;
            border: 1px solid var(--border-color);
        }

        .timeline-header {
            font-size: 0.75rem;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-bottom: 0.75rem;
        }

        .timeline {
            display: flex;
            gap: 0.5rem;
            align-items: flex-end;
            height: 60px;
        }

        .timeline-bar {
            flex: 1;
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 0.25rem;
        }

        .timeline-bar-fill {
            width: 100%;
            border-radius: 4px 4px 0 0;
            transition: height 0.3s ease;
            position: relative;
            display: flex;
            flex-direction: column;
            justify-content: flex-end;
        }

        .timeline-bar-segment {
            width: 100%;
            transition: height 0.3s ease;
        }

        .timeline-bar-segment.critical { background: var(--critical); }
        .timeline-bar-segment.warning { background: var(--warning); }
        .timeline-bar-segment.info { background: var(--info); }
        .timeline-bar-segment.healthy { background: var(--success); opacity: 0.3; }

        .timeline-time {
            font-size: 0.625rem;
            color: var(--text-muted);
        }

        /* ========================================
           Namespace Info
           ======================================== */
        .namespace-info {
            margin-bottom: 1.5rem;
            padding: 0.75rem 1rem;
            background: var(--bg-secondary);
            border-radius: 8px;
            border: 1px solid var(--border-color);
            display: flex;
            align-items: center;
            gap: 0.5rem;
            flex-wrap: wrap;
        }

        .namespace-info .label {
            color: var(--text-secondary);
            font-size: 0.875rem;
        }

        .namespace-tag {
            background: var(--bg-card);
            padding: 0.25rem 0.5rem;
            border-radius: 4px;
            font-size: 0.75rem;
            font-family: monospace;
        }

        /* ========================================
           Checkers Grid
           ======================================== */
        .checkers-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(450px, 1fr));
            gap: 1.5rem;
        }

        .checker-card {
            background: var(--bg-secondary);
            border-radius: 12px;
            border: 1px solid var(--border-color);
            overflow: hidden;
            transition: all 0.2s ease;
        }

        .checker-card:hover {
            box-shadow: 0 4px 20px var(--shadow-color);
        }

        .checker-card.has-critical {
            border-color: var(--critical);
            animation: critical-pulse 2s infinite;
        }

        .checker-header {
            padding: 1rem 1.25rem;
            display: flex;
            justify-content: space-between;
            align-items: center;
            border-bottom: 1px solid var(--border-color);
            cursor: pointer;
            transition: background 0.2s ease;
            user-select: none;
        }

        .checker-header:hover {
            background: var(--bg-card);
        }

        .checker-header-left {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .checker-icon {
            width: 32px;
            height: 32px;
            border-radius: 8px;
            display: flex;
            align-items: center;
            justify-content: center;
            background: var(--bg-card);
        }

        .checker-icon svg {
            width: 18px;
            height: 18px;
            color: var(--text-secondary);
        }

        .checker-name {
            font-weight: 600;
            text-transform: capitalize;
        }

        .checker-stats {
            display: flex;
            gap: 0.75rem;
            font-size: 0.75rem;
        }

        .checker-stat {
            display: flex;
            align-items: center;
            gap: 0.25rem;
            padding: 0.25rem 0.5rem;
            border-radius: 4px;
            background: var(--bg-card);
        }

        .checker-stat.healthy { color: var(--success); }
        .checker-stat.critical { color: var(--critical); background: var(--critical-bg); }
        .checker-stat.warning { color: var(--warning); background: var(--warning-bg); }
        .checker-stat.info { color: var(--info); background: var(--info-bg); }

        .collapse-icon {
            transition: transform 0.2s ease;
            color: var(--text-muted);
        }

        .checker-card.collapsed .collapse-icon {
            transform: rotate(-90deg);
        }

        .issues-list {
            max-height: 400px;
            overflow-y: auto;
            transition: max-height 0.3s ease, opacity 0.3s ease;
        }

        .checker-card.collapsed .issues-list {
            max-height: 0;
            opacity: 0;
            overflow: hidden;
        }

        .issue {
            padding: 1rem 1.25rem;
            border-bottom: 1px solid var(--border-color);
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
            transition: background 0.2s ease;
            animation: fade-in 0.3s ease;
        }

        .issue:last-child {
            border-bottom: none;
        }

        .issue:hover {
            background: var(--bg-card);
        }

        .issue.highlight {
            background: rgba(59, 130, 246, 0.1);
        }

        .issue-header {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .severity-badge {
            padding: 0.25rem 0.5rem;
            border-radius: 4px;
            font-size: 0.7rem;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.025em;
        }

        .severity-badge.critical {
            background: var(--critical-bg);
            color: var(--critical);
        }

        .severity-badge.warning {
            background: var(--warning-bg);
            color: var(--warning);
        }

        .severity-badge.info {
            background: var(--info-bg);
            color: var(--info);
        }

        .issue-type {
            font-weight: 500;
            font-size: 0.9rem;
        }

        .issue-resource {
            font-family: 'SF Mono', Monaco, 'Cascadia Code', monospace;
            font-size: 0.8rem;
            color: var(--text-secondary);
            background: var(--bg-card);
            padding: 0.125rem 0.375rem;
            border-radius: 4px;
            display: inline-block;
        }

        .issue-message {
            font-size: 0.875rem;
            color: var(--text-secondary);
            line-height: 1.5;
        }

        .issue-suggestion {
            font-size: 0.8rem;
            color: var(--accent-blue);
            display: flex;
            align-items: flex-start;
            gap: 0.375rem;
        }

        .issue-suggestion svg {
            width: 14px;
            height: 14px;
            flex-shrink: 0;
            margin-top: 2px;
        }

        .no-issues {
            padding: 2rem;
            text-align: center;
            color: var(--text-secondary);
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 0.5rem;
        }

        .no-issues svg {
            width: 32px;
            height: 32px;
            color: var(--success);
        }

        /* ========================================
           Loading & Skeleton
           ======================================== */
        .connecting {
            text-align: center;
            padding: 4rem 2rem;
            color: var(--text-secondary);
        }

        .spinner {
            width: 48px;
            height: 48px;
            border: 3px solid var(--bg-card);
            border-top-color: var(--accent-blue);
            border-radius: 50%;
            animation: spin 1s linear infinite;
            margin: 0 auto 1.5rem;
        }

        .skeleton {
            background: linear-gradient(90deg,
                var(--skeleton-base) 0%,
                var(--skeleton-shine) 50%,
                var(--skeleton-base) 100%
            );
            background-size: 200% 100%;
            animation: skeleton-shimmer 1.5s ease-in-out infinite;
            border-radius: 8px;
        }

        .skeleton-card {
            height: 200px;
            border-radius: 12px;
        }

        .skeleton-text {
            height: 1rem;
            margin-bottom: 0.5rem;
        }

        .skeleton-text.short { width: 40%; }
        .skeleton-text.medium { width: 70%; }

        /* ========================================
           Modal (Keyboard Shortcuts)
           ======================================== */
        .modal-overlay {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(0, 0, 0, 0.6);
            display: flex;
            align-items: center;
            justify-content: center;
            z-index: 1000;
            opacity: 0;
            visibility: hidden;
            transition: opacity 0.2s ease, visibility 0.2s ease;
        }

        .modal-overlay.active {
            opacity: 1;
            visibility: visible;
        }

        .modal {
            background: var(--bg-secondary);
            border-radius: 16px;
            border: 1px solid var(--border-color);
            width: 90%;
            max-width: 500px;
            max-height: 80vh;
            overflow: auto;
            transform: scale(0.95);
            transition: transform 0.2s ease;
        }

        .modal-overlay.active .modal {
            transform: scale(1);
        }

        .modal-header {
            padding: 1.25rem 1.5rem;
            border-bottom: 1px solid var(--border-color);
            display: flex;
            justify-content: space-between;
            align-items: center;
        }

        .modal-header h2 {
            font-size: 1.125rem;
            font-weight: 600;
        }

        .modal-close {
            background: none;
            border: none;
            color: var(--text-secondary);
            cursor: pointer;
            padding: 0.25rem;
            display: flex;
            align-items: center;
            justify-content: center;
        }

        .modal-close:hover {
            color: var(--text-primary);
        }

        .modal-body {
            padding: 1.5rem;
        }

        .shortcut-list {
            display: flex;
            flex-direction: column;
            gap: 0.75rem;
        }

        .shortcut-item {
            display: flex;
            justify-content: space-between;
            align-items: center;
        }

        .shortcut-key {
            display: inline-flex;
            align-items: center;
            gap: 0.25rem;
        }

        .shortcut-key kbd {
            background: var(--bg-card);
            padding: 0.25rem 0.5rem;
            border-radius: 4px;
            font-family: 'SF Mono', Monaco, monospace;
            font-size: 0.75rem;
            border: 1px solid var(--border-color);
        }

        .shortcut-desc {
            color: var(--text-secondary);
            font-size: 0.875rem;
        }

        /* ========================================
           Timestamp
           ======================================== */
        .timestamp {
            text-align: center;
            padding: 1.5rem;
            font-size: 0.75rem;
            color: var(--text-muted);
        }

        /* ========================================
           Responsive
           ======================================== */
        @media (max-width: 900px) {
            .header {
                flex-direction: column;
                gap: 1rem;
                padding: 1rem;
            }

            .toolbar {
                flex-direction: column;
            }

            .search-box {
                width: 100%;
            }

            .severity-tabs {
                width: 100%;
                justify-content: center;
            }

            .export-group {
                margin-left: 0;
                width: 100%;
                justify-content: center;
            }

            .summary-section {
                grid-template-columns: 1fr;
            }

            .health-score-card {
                justify-self: center;
            }

            .checkers-grid {
                grid-template-columns: 1fr;
            }

            .container {
                padding: 1rem;
            }
        }

        /* ========================================
           Scrollbar
           ======================================== */
        ::-webkit-scrollbar {
            width: 8px;
            height: 8px;
        }

        ::-webkit-scrollbar-track {
            background: var(--bg-primary);
        }

        ::-webkit-scrollbar-thumb {
            background: var(--bg-card);
            border-radius: 4px;
        }

        ::-webkit-scrollbar-thumb:hover {
            background: var(--bg-hover);
        }
    </style>
</head>
<body>
    <header class="header">
        <div class="header-left">
            <h1>Team Health Dashboard</h1>
            <span class="issue-badge healthy-badge" id="totalBadge">0 issues</span>
        </div>
        <div class="header-controls">
            <div class="status">
                <div class="status-dot" id="connectionDot"></div>
                <span id="connectionStatus">Connecting...</span>
            </div>
            <button class="btn btn-icon" id="pauseBtn" title="Pause updates (P)">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="6" y="4" width="4" height="16"></rect>
                    <rect x="14" y="4" width="4" height="16"></rect>
                </svg>
            </button>
            <button class="btn btn-icon" id="themeBtn" title="Toggle theme (T)">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="5"></circle>
                    <line x1="12" y1="1" x2="12" y2="3"></line>
                    <line x1="12" y1="21" x2="12" y2="23"></line>
                    <line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line>
                    <line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line>
                    <line x1="1" y1="12" x2="3" y2="12"></line>
                    <line x1="21" y1="12" x2="23" y2="12"></line>
                    <line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line>
                    <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line>
                </svg>
            </button>
            <button class="btn btn-icon" id="helpBtn" title="Keyboard shortcuts (?)">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="10"></circle>
                    <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"></path>
                    <line x1="12" y1="17" x2="12.01" y2="17"></line>
                </svg>
            </button>
            <button class="btn btn-primary" onclick="triggerCheck()">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="23 4 23 10 17 10"></polyline>
                    <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
                </svg>
                Refresh
            </button>
        </div>
    </header>

    <main class="container">
        <div id="loading" class="connecting">
            <div class="spinner"></div>
            <p>Connecting to health monitor...</p>
        </div>

        <div id="skeletons" style="display: none;">
            <div class="summary-section">
                <div class="skeleton skeleton-card" style="width: 180px; height: 180px;"></div>
                <div class="summary-cards">
                    <div class="skeleton skeleton-card" style="height: 100px;"></div>
                    <div class="skeleton skeleton-card" style="height: 100px;"></div>
                    <div class="skeleton skeleton-card" style="height: 100px;"></div>
                    <div class="skeleton skeleton-card" style="height: 100px;"></div>
                </div>
            </div>
            <div class="checkers-grid">
                <div class="skeleton skeleton-card"></div>
                <div class="skeleton skeleton-card"></div>
                <div class="skeleton skeleton-card"></div>
                <div class="skeleton skeleton-card"></div>
            </div>
        </div>

        <div id="dashboard" style="display: none;">
            <!-- Toolbar -->
            <div class="toolbar">
                <div class="search-box">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <circle cx="11" cy="11" r="8"></circle>
                        <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
                    </svg>
                    <input type="text" id="searchInput" placeholder="Search resources... (Press /)" autocomplete="off">
                </div>
                <div class="filter-group">
                    <select class="filter-select" id="namespaceFilter">
                        <option value="">All Namespaces</option>
                    </select>
                    <select class="filter-select" id="checkerFilter">
                        <option value="">All Checkers</option>
                    </select>
                </div>
                <div class="severity-tabs" id="severityTabs">
                    <button class="severity-tab active" data-severity="all">
                        All <span class="count" id="countAll">0</span>
                    </button>
                    <button class="severity-tab" data-severity="critical">
                        Critical <span class="count" id="countCritical">0</span>
                    </button>
                    <button class="severity-tab" data-severity="warning">
                        Warning <span class="count" id="countWarning">0</span>
                    </button>
                    <button class="severity-tab" data-severity="info">
                        Info <span class="count" id="countInfo">0</span>
                    </button>
                </div>
                <div class="export-group">
                    <button class="btn" onclick="exportJSON()" title="Export as JSON">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                            <polyline points="7 10 12 15 17 10"></polyline>
                            <line x1="12" y1="15" x2="12" y2="3"></line>
                        </svg>
                        JSON
                    </button>
                    <button class="btn" onclick="exportCSV()" title="Export as CSV">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                            <polyline points="7 10 12 15 17 10"></polyline>
                            <line x1="12" y1="15" x2="12" y2="3"></line>
                        </svg>
                        CSV
                    </button>
                </div>
            </div>

            <!-- Summary Section with Health Score -->
            <div class="summary-section">
                <div class="health-score-card">
                    <div class="progress-ring-container">
                        <svg class="progress-ring" width="120" height="120">
                            <circle class="progress-ring-bg" cx="60" cy="60" r="45"></circle>
                            <circle class="progress-ring-fill" id="progressRing" cx="60" cy="60" r="45"></circle>
                        </svg>
                        <div class="progress-ring-text">
                            <div class="progress-ring-value" id="healthScore">--</div>
                            <div class="progress-ring-label">Health</div>
                        </div>
                    </div>
                    <div class="health-score-title">Overall Health Score</div>
                </div>
                <div class="summary-cards">
                    <div class="summary-card healthy">
                        <div class="label">Healthy</div>
                        <div class="value" id="healthyCount">0</div>
                    </div>
                    <div class="summary-card critical">
                        <div class="label">Critical</div>
                        <div class="value" id="criticalCount">0</div>
                    </div>
                    <div class="summary-card warning">
                        <div class="label">Warning</div>
                        <div class="value" id="warningCount">0</div>
                    </div>
                    <div class="summary-card info">
                        <div class="label">Info</div>
                        <div class="value" id="infoCount">0</div>
                    </div>
                </div>
            </div>

            <!-- Timeline -->
            <div class="timeline-section">
                <div class="timeline-header">Check History (Last 5)</div>
                <div class="timeline" id="timeline">
                    <div class="timeline-bar">
                        <div class="timeline-bar-fill" style="height: 100%; background: var(--bg-card);"></div>
                        <div class="timeline-time">--</div>
                    </div>
                </div>
            </div>

            <!-- Namespace Info -->
            <div class="namespace-info">
                <span class="label">Monitoring:</span>
                <div id="namespaceList"></div>
            </div>

            <!-- Checkers Grid -->
            <div class="checkers-grid" id="checkersGrid"></div>

            <!-- Timestamp -->
            <div class="timestamp" id="timestamp"></div>
        </div>
    </main>

    <!-- Keyboard Shortcuts Modal -->
    <div class="modal-overlay" id="helpModal">
        <div class="modal">
            <div class="modal-header">
                <h2>Keyboard Shortcuts</h2>
                <button class="modal-close" onclick="toggleHelpModal()">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <line x1="18" y1="6" x2="6" y2="18"></line>
                        <line x1="6" y1="6" x2="18" y2="18"></line>
                    </svg>
                </button>
            </div>
            <div class="modal-body">
                <div class="shortcut-list">
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>/</kbd></span>
                        <span class="shortcut-desc">Focus search</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>j</kbd></span>
                        <span class="shortcut-desc">Next issue</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>k</kbd></span>
                        <span class="shortcut-desc">Previous issue</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>f</kbd></span>
                        <span class="shortcut-desc">Focus namespace filter</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>t</kbd></span>
                        <span class="shortcut-desc">Toggle theme</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>p</kbd></span>
                        <span class="shortcut-desc">Pause/resume updates</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>r</kbd></span>
                        <span class="shortcut-desc">Refresh data</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>1</kbd> <kbd>2</kbd> <kbd>3</kbd> <kbd>4</kbd></span>
                        <span class="shortcut-desc">Filter by severity</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>?</kbd></span>
                        <span class="shortcut-desc">Toggle this help</span>
                    </div>
                    <div class="shortcut-item">
                        <span class="shortcut-key"><kbd>Esc</kbd></span>
                        <span class="shortcut-desc">Close modal / blur input</span>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        // ========================================
        // HTML Escaping (XSS Prevention)
        // ========================================
        function escapeHTML(str) {
            if (typeof str !== 'string') return str;
            return str
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;')
                .replace(/'/g, '&#39;');
        }

        // ========================================
        // State Management
        // ========================================
        const state = {
            data: null,
            history: [],
            isPaused: false,
            theme: localStorage.getItem('kubeassist-theme') || 'dark',
            filters: {
                namespace: '',
                checker: '',
                severity: 'all',
                search: ''
            },
            collapsed: new Set(JSON.parse(localStorage.getItem('kubeassist-collapsed') || '[]')),
            currentIssueIndex: -1
        };

        let eventSource = null;

        // ========================================
        // Initialization
        // ========================================
        function init() {
            applyTheme();
            initKeyboardShortcuts();
            initEventListeners();
            connect();
        }

        function initEventListeners() {
            // Search
            document.getElementById('searchInput').addEventListener('input', (e) => {
                state.filters.search = e.target.value.toLowerCase();
                renderDashboard();
            });

            // Filters
            document.getElementById('namespaceFilter').addEventListener('change', (e) => {
                state.filters.namespace = e.target.value;
                renderDashboard();
            });

            document.getElementById('checkerFilter').addEventListener('change', (e) => {
                state.filters.checker = e.target.value;
                renderDashboard();
            });

            // Severity tabs
            document.querySelectorAll('.severity-tab').forEach(tab => {
                tab.addEventListener('click', () => {
                    document.querySelectorAll('.severity-tab').forEach(t => t.classList.remove('active'));
                    tab.classList.add('active');
                    state.filters.severity = tab.dataset.severity;
                    renderDashboard();
                });
            });

            // Theme button
            document.getElementById('themeBtn').addEventListener('click', toggleTheme);

            // Pause button
            document.getElementById('pauseBtn').addEventListener('click', togglePause);

            // Help button
            document.getElementById('helpBtn').addEventListener('click', toggleHelpModal);

            // Modal close on overlay click
            document.getElementById('helpModal').addEventListener('click', (e) => {
                if (e.target.id === 'helpModal') toggleHelpModal();
            });
        }

        // ========================================
        // Keyboard Shortcuts
        // ========================================
        function initKeyboardShortcuts() {
            document.addEventListener('keydown', (e) => {
                // Ignore when typing in inputs
                if (e.target.tagName === 'INPUT' || e.target.tagName === 'SELECT') {
                    if (e.key === 'Escape') {
                        e.target.blur();
                    }
                    return;
                }

                switch (e.key) {
                    case '/':
                        e.preventDefault();
                        document.getElementById('searchInput').focus();
                        break;
                    case 'j':
                        navigateIssue(1);
                        break;
                    case 'k':
                        navigateIssue(-1);
                        break;
                    case 'f':
                        document.getElementById('namespaceFilter').focus();
                        break;
                    case 't':
                        toggleTheme();
                        break;
                    case 'p':
                        togglePause();
                        break;
                    case 'r':
                        triggerCheck();
                        break;
                    case '?':
                        toggleHelpModal();
                        break;
                    case '1':
                        selectSeverityTab('all');
                        break;
                    case '2':
                        selectSeverityTab('critical');
                        break;
                    case '3':
                        selectSeverityTab('warning');
                        break;
                    case '4':
                        selectSeverityTab('info');
                        break;
                    case 'Escape':
                        if (document.getElementById('helpModal').classList.contains('active')) {
                            toggleHelpModal();
                        }
                        break;
                }
            });
        }

        function selectSeverityTab(severity) {
            document.querySelectorAll('.severity-tab').forEach(t => t.classList.remove('active'));
            document.querySelector(` + "`" + `.severity-tab[data-severity="${severity}"]` + "`" + `).classList.add('active');
            state.filters.severity = severity;
            renderDashboard();
        }

        function navigateIssue(direction) {
            const issues = document.querySelectorAll('.issue');
            if (issues.length === 0) return;

            // Remove previous highlight
            issues.forEach(i => i.classList.remove('highlight'));

            state.currentIssueIndex += direction;
            if (state.currentIssueIndex < 0) state.currentIssueIndex = issues.length - 1;
            if (state.currentIssueIndex >= issues.length) state.currentIssueIndex = 0;

            const currentIssue = issues[state.currentIssueIndex];
            currentIssue.classList.add('highlight');
            currentIssue.scrollIntoView({ behavior: 'smooth', block: 'center' });
        }

        // ========================================
        // Theme Management
        // ========================================
        function applyTheme() {
            document.documentElement.setAttribute('data-theme', state.theme);
            updateThemeIcon();
        }

        function toggleTheme() {
            state.theme = state.theme === 'dark' ? 'light' : 'dark';
            localStorage.setItem('kubeassist-theme', state.theme);
            applyTheme();
        }

        function updateThemeIcon() {
            const btn = document.getElementById('themeBtn');
            if (state.theme === 'light') {
                btn.innerHTML = ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path>
                </svg>` + "`" + `;
            } else {
                btn.innerHTML = ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="5"></circle>
                    <line x1="12" y1="1" x2="12" y2="3"></line>
                    <line x1="12" y1="21" x2="12" y2="23"></line>
                    <line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line>
                    <line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line>
                    <line x1="1" y1="12" x2="3" y2="12"></line>
                    <line x1="21" y1="12" x2="23" y2="12"></line>
                    <line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line>
                    <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line>
                </svg>` + "`" + `;
            }
        }

        // ========================================
        // Pause/Resume
        // ========================================
        function togglePause() {
            state.isPaused = !state.isPaused;
            const btn = document.getElementById('pauseBtn');
            const dot = document.getElementById('connectionDot');
            const status = document.getElementById('connectionStatus');

            if (state.isPaused) {
                btn.innerHTML = ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polygon points="5 3 19 12 5 21 5 3"></polygon>
                </svg>` + "`" + `;
                btn.title = 'Resume updates (P)';
                dot.classList.add('paused');
                status.textContent = 'Paused';
                if (eventSource) {
                    eventSource.close();
                    eventSource = null;
                }
            } else {
                btn.innerHTML = ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="6" y="4" width="4" height="16"></rect>
                    <rect x="14" y="4" width="4" height="16"></rect>
                </svg>` + "`" + `;
                btn.title = 'Pause updates (P)';
                dot.classList.remove('paused');
                connect();
            }
        }

        // ========================================
        // Help Modal
        // ========================================
        function toggleHelpModal() {
            document.getElementById('helpModal').classList.toggle('active');
        }

        // ========================================
        // SSE Connection
        // ========================================
        async function fetchInitialData() {
            try {
                const resp = await fetch('/api/health');
                const data = await resp.json();
                if (data && data.results) {
                    updateData(data);
                } else {
                    showSkeletons();
                }
            } catch (e) {
                console.log('Initial fetch failed, waiting for SSE');
                showSkeletons();
            }
        }

        function showSkeletons() {
            document.getElementById('loading').style.display = 'none';
            document.getElementById('skeletons').style.display = 'block';
        }

        function connect() {
            if (state.isPaused) return;

            fetchInitialData();

            eventSource = new EventSource('/api/events');

            eventSource.onopen = () => {
                document.getElementById('connectionDot').style.background = '';
                document.getElementById('connectionDot').classList.remove('disconnected');
                document.getElementById('connectionStatus').textContent = 'Connected';
            };

            eventSource.onmessage = (event) => {
                if (state.isPaused) return;
                const data = JSON.parse(event.data);
                updateData(data);
            };

            eventSource.onerror = () => {
                document.getElementById('connectionDot').classList.add('disconnected');
                document.getElementById('connectionStatus').textContent = 'Disconnected';
                if (!state.isPaused) {
                    setTimeout(connect, 5000);
                }
            };
        }

        // ========================================
        // Data Update
        // ========================================
        function updateData(data) {
            state.data = data;
            addToHistory(data);
            populateFilters(data);
            renderDashboard();
        }

        function addToHistory(data) {
            const historyEntry = {
                timestamp: new Date(data.timestamp),
                critical: data.summary.criticalCount,
                warning: data.summary.warningCount,
                info: data.summary.infoCount,
                healthy: data.summary.totalHealthy
            };

            state.history.push(historyEntry);
            if (state.history.length > 5) {
                state.history.shift();
            }
        }

        function populateFilters(data) {
            // Namespaces
            const nsSelect = document.getElementById('namespaceFilter');
            const currentNs = nsSelect.value;
            const namespaces = data.namespaces || [];

            nsSelect.innerHTML = '<option value="">All Namespaces</option>';
            namespaces.forEach(ns => {
                const opt = document.createElement('option');
                opt.value = ns;
                opt.textContent = ns;
                nsSelect.appendChild(opt);
            });
            nsSelect.value = currentNs;

            // Checkers
            const checkerSelect = document.getElementById('checkerFilter');
            const currentChecker = checkerSelect.value;
            const checkers = Object.keys(data.results || {});

            checkerSelect.innerHTML = '<option value="">All Checkers</option>';
            checkers.forEach(checker => {
                const opt = document.createElement('option');
                opt.value = checker;
                opt.textContent = checker.charAt(0).toUpperCase() + checker.slice(1);
                checkerSelect.appendChild(opt);
            });
            checkerSelect.value = currentChecker;
        }

        // ========================================
        // Rendering
        // ========================================
        function renderDashboard() {
            if (!state.data) return;

            document.getElementById('loading').style.display = 'none';
            document.getElementById('skeletons').style.display = 'none';
            document.getElementById('dashboard').style.display = 'block';

            const data = state.data;
            const filteredResults = applyFilters(data);

            // Calculate filtered counts
            let filteredCritical = 0, filteredWarning = 0, filteredInfo = 0;
            Object.values(filteredResults).forEach(result => {
                (result.issues || []).forEach(issue => {
                    const sev = issue.severity.toLowerCase();
                    if (sev === 'critical') filteredCritical++;
                    else if (sev === 'warning') filteredWarning++;
                    else if (sev === 'info') filteredInfo++;
                });
            });

            // Update summary
            document.getElementById('healthyCount').textContent = data.summary.totalHealthy;
            document.getElementById('criticalCount').textContent = data.summary.criticalCount;
            document.getElementById('warningCount').textContent = data.summary.warningCount;
            document.getElementById('infoCount').textContent = data.summary.infoCount;

            // Update severity tab counts
            const totalIssues = data.summary.criticalCount + data.summary.warningCount + data.summary.infoCount;
            document.getElementById('countAll').textContent = totalIssues;
            document.getElementById('countCritical').textContent = data.summary.criticalCount;
            document.getElementById('countWarning').textContent = data.summary.warningCount;
            document.getElementById('countInfo').textContent = data.summary.infoCount;

            // Update header badge
            updateHeaderBadge(totalIssues, data.summary.criticalCount);

            // Update health score
            updateHealthScore(data);

            // Update timeline
            renderTimeline();

            // Update namespace list
            renderNamespaces(data.namespaces || []);

            // Update checkers grid
            renderCheckers(filteredResults);

            // Update timestamp
            const ts = new Date(data.timestamp);
            document.getElementById('timestamp').textContent = 'Last updated: ' + ts.toLocaleTimeString();
        }

        function applyFilters(data) {
            const filtered = {};
            const { namespace, checker, severity, search } = state.filters;

            for (const [name, result] of Object.entries(data.results || {})) {
                // Checker filter
                if (checker && name !== checker) continue;

                const filteredIssues = (result.issues || []).filter(issue => {
                    // Namespace filter
                    if (namespace && issue.namespace !== namespace) return false;

                    // Severity filter
                    if (severity !== 'all' && issue.severity.toLowerCase() !== severity) return false;

                    // Search filter
                    if (search) {
                        const searchStr = ` + "`" + `${issue.resource} ${issue.namespace} ${issue.message} ${issue.type}` + "`" + `.toLowerCase();
                        if (!searchStr.includes(search)) return false;
                    }

                    return true;
                });

                filtered[name] = {
                    ...result,
                    issues: filteredIssues
                };
            }

            return filtered;
        }

        function updateHeaderBadge(total, critical) {
            const badge = document.getElementById('totalBadge');
            badge.textContent = ` + "`" + `${total} issue${total !== 1 ? 's' : ''}` + "`" + `;

            badge.classList.remove('healthy-badge', 'warning-badge');
            if (total === 0) {
                badge.classList.add('healthy-badge');
                badge.textContent = 'All healthy';
            } else if (critical > 0) {
                // Default red with pulse animation
            } else {
                badge.classList.add('warning-badge');
            }
        }

        function updateHealthScore(data) {
            const { totalHealthy, criticalCount, warningCount, infoCount } = data.summary;
            const totalResources = totalHealthy + criticalCount + warningCount + infoCount;

            if (totalResources === 0) {
                document.getElementById('healthScore').textContent = '--';
                return;
            }

            const weightedIssues = (criticalCount * 3) + (warningCount * 2) + (infoCount * 1);
            const maxWeight = totalResources * 3;
            const score = Math.max(0, Math.round(100 * (1 - weightedIssues / maxWeight)));

            document.getElementById('healthScore').textContent = score;

            // Update progress ring
            const ring = document.getElementById('progressRing');
            const circumference = 2 * Math.PI * 45; // r=45
            const offset = circumference - (score / 100) * circumference;
            ring.style.strokeDashoffset = offset;

            // Color based on score
            ring.classList.remove('warning', 'critical');
            if (score < 50) {
                ring.classList.add('critical');
            } else if (score < 80) {
                ring.classList.add('warning');
            }
        }

        function renderTimeline() {
            const container = document.getElementById('timeline');
            container.innerHTML = '';

            const maxHeight = 50;

            // Fill with empty bars if less than 5 entries
            const entries = [...state.history];
            while (entries.length < 5) {
                entries.unshift(null);
            }

            entries.forEach((entry, i) => {
                const bar = document.createElement('div');
                bar.className = 'timeline-bar';

                if (!entry) {
                    bar.innerHTML = ` + "`" + `
                        <div class="timeline-bar-fill" style="height: ${maxHeight}px; background: var(--bg-card);"></div>
                        <div class="timeline-time">--</div>
                    ` + "`" + `;
                } else {
                    const total = entry.critical + entry.warning + entry.info + entry.healthy;
                    const criticalH = total > 0 ? (entry.critical / total) * maxHeight : 0;
                    const warningH = total > 0 ? (entry.warning / total) * maxHeight : 0;
                    const infoH = total > 0 ? (entry.info / total) * maxHeight : 0;
                    const healthyH = maxHeight - criticalH - warningH - infoH;

                    bar.innerHTML = ` + "`" + `
                        <div class="timeline-bar-fill" style="height: ${maxHeight}px; display: flex; flex-direction: column-reverse;">
                            ${healthyH > 0 ? ` + "`" + `<div class="timeline-bar-segment healthy" style="height: ${healthyH}px;"></div>` + "`" + ` : ''}
                            ${infoH > 0 ? ` + "`" + `<div class="timeline-bar-segment info" style="height: ${infoH}px;"></div>` + "`" + ` : ''}
                            ${warningH > 0 ? ` + "`" + `<div class="timeline-bar-segment warning" style="height: ${warningH}px;"></div>` + "`" + ` : ''}
                            ${criticalH > 0 ? ` + "`" + `<div class="timeline-bar-segment critical" style="height: ${criticalH}px;"></div>` + "`" + ` : ''}
                        </div>
                        <div class="timeline-time">${entry.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</div>
                    ` + "`" + `;
                }

                container.appendChild(bar);
            });
        }

        function renderNamespaces(namespaces) {
            const container = document.getElementById('namespaceList');
            container.innerHTML = namespaces.length > 0
                ? namespaces.map(ns => ` + "`" + `<span class="namespace-tag">${escapeHTML(ns)}</span>` + "`" + `).join('')
                : '<span class="namespace-tag">none</span>';
        }

        function renderCheckers(results) {
            const grid = document.getElementById('checkersGrid');
            grid.innerHTML = '';

            // Sort by issue count (most issues first)
            const sorted = Object.entries(results).sort((a, b) => {
                const aIssues = (a[1].issues || []).length;
                const bIssues = (b[1].issues || []).length;
                return bIssues - aIssues;
            });

            for (const [name, result] of sorted) {
                const card = createCheckerCard(name, result);
                grid.appendChild(card);
            }
        }

        function createCheckerCard(name, result) {
            const card = document.createElement('div');
            card.className = 'checker-card';
            if (state.collapsed.has(name)) {
                card.classList.add('collapsed');
            }

            const issues = result.issues || [];
            const criticalCount = issues.filter(i => i.severity.toLowerCase() === 'critical').length;
            const warningCount = issues.filter(i => i.severity.toLowerCase() === 'warning').length;
            const infoCount = issues.filter(i => i.severity.toLowerCase() === 'info').length;

            if (criticalCount > 0) {
                card.classList.add('has-critical');
            }

            const icon = getCheckerIcon(name);

            card.innerHTML = ` + "`" + `
                <div class="checker-header" onclick="toggleChecker('${escapeHTML(name)}')">
                    <div class="checker-header-left">
                        <div class="checker-icon">${icon}</div>
                        <span class="checker-name">${escapeHTML(name)}</span>
                    </div>
                    <div style="display: flex; align-items: center; gap: 0.75rem;">
                        <div class="checker-stats">
                            <span class="checker-stat healthy">${result.healthy} healthy</span>
                            ${criticalCount > 0 ? ` + "`" + `<span class="checker-stat critical">${criticalCount} critical</span>` + "`" + ` : ''}
                            ${warningCount > 0 ? ` + "`" + `<span class="checker-stat warning">${warningCount} warning</span>` + "`" + ` : ''}
                            ${infoCount > 0 ? ` + "`" + `<span class="checker-stat info">${infoCount} info</span>` + "`" + ` : ''}
                        </div>
                        <svg class="collapse-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <polyline points="6 9 12 15 18 9"></polyline>
                        </svg>
                    </div>
                </div>
                <div class="issues-list">
                    ${issues.length === 0 ?
                        ` + "`" + `<div class="no-issues">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path>
                                <polyline points="22 4 12 14.01 9 11.01"></polyline>
                            </svg>
                            All resources healthy
                        </div>` + "`" + ` :
                        issues.map(issue => createIssueHTML(issue)).join('')
                    }
                </div>
            ` + "`" + `;

            return card;
        }

        function getCheckerIcon(name) {
            const icons = {
                workloads: ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="2" y="2" width="8" height="8" rx="1"></rect>
                    <rect x="14" y="2" width="8" height="8" rx="1"></rect>
                    <rect x="2" y="14" width="8" height="8" rx="1"></rect>
                    <rect x="14" y="14" width="8" height="8" rx="1"></rect>
                </svg>` + "`" + `,
                secrets: ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
                    <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
                </svg>` + "`" + `,
                pvcs: ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <ellipse cx="12" cy="5" rx="9" ry="3"></ellipse>
                    <path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"></path>
                    <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"></path>
                </svg>` + "`" + `,
                quotas: ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"></path>
                </svg>` + "`" + `,
                networkpolicies: ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>
                </svg>` + "`" + `,
                helmreleases: ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="10"></circle>
                    <path d="M16 8l-8 8M8 8l8 8"></path>
                </svg>` + "`" + `,
                kustomizations: ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polygon points="12 2 2 7 12 12 22 7 12 2"></polygon>
                    <polyline points="2 17 12 22 22 17"></polyline>
                    <polyline points="2 12 12 17 22 12"></polyline>
                </svg>` + "`" + `,
                gitrepositories: ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="4"></circle>
                    <line x1="1.05" y1="12" x2="7" y2="12"></line>
                    <line x1="17.01" y1="12" x2="22.96" y2="12"></line>
                </svg>` + "`" + `
            };
            return icons[name] || ` + "`" + `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="12" cy="12" r="10"></circle>
            </svg>` + "`" + `;
        }

        function createIssueHTML(issue) {
            const severityLower = issue.severity.toLowerCase();
            return ` + "`" + `
                <div class="issue">
                    <div class="issue-header">
                        <span class="severity-badge ${severityLower}">${escapeHTML(issue.severity)}</span>
                        <span class="issue-type">${escapeHTML(issue.type)}</span>
                    </div>
                    <div class="issue-resource">${escapeHTML(issue.namespace)}/${escapeHTML(issue.resource)}</div>
                    <div class="issue-message">${escapeHTML(issue.message)}</div>
                    ${issue.suggestion ? ` + "`" + `
                        <div class="issue-suggestion">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <circle cx="12" cy="12" r="10"></circle>
                                <line x1="12" y1="16" x2="12" y2="12"></line>
                                <line x1="12" y1="8" x2="12.01" y2="8"></line>
                            </svg>
                            ${escapeHTML(issue.suggestion)}
                        </div>
                    ` + "`" + ` : ''}
                </div>
            ` + "`" + `;
        }

        function toggleChecker(name) {
            if (state.collapsed.has(name)) {
                state.collapsed.delete(name);
            } else {
                state.collapsed.add(name);
            }
            localStorage.setItem('kubeassist-collapsed', JSON.stringify([...state.collapsed]));
            renderDashboard();
        }

        // ========================================
        // Export Functions
        // ========================================
        function exportJSON() {
            if (!state.data) return;

            const blob = new Blob([JSON.stringify(state.data, null, 2)], { type: 'application/json' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = ` + "`" + `health-report-${new Date().toISOString().split('T')[0]}.json` + "`" + `;
            a.click();
            URL.revokeObjectURL(url);
        }

        function exportCSV() {
            if (!state.data) return;

            const rows = [['Checker', 'Severity', 'Type', 'Namespace', 'Resource', 'Message', 'Suggestion']];

            for (const [checker, result] of Object.entries(state.data.results || {})) {
                for (const issue of (result.issues || [])) {
                    rows.push([
                        checker,
                        issue.severity,
                        issue.type,
                        issue.namespace,
                        issue.resource,
                        ` + "`" + `"${(issue.message || '').replace(/"/g, '""')}"` + "`" + `,
                        ` + "`" + `"${(issue.suggestion || '').replace(/"/g, '""')}"` + "`" + `
                    ]);
                }
            }

            const csv = rows.map(row => row.join(',')).join('\\n');
            const blob = new Blob([csv], { type: 'text/csv' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = ` + "`" + `health-report-${new Date().toISOString().split('T')[0]}.csv` + "`" + `;
            a.click();
            URL.revokeObjectURL(url);
        }

        // ========================================
        // API Interactions
        // ========================================
        async function triggerCheck() {
            try {
                await fetch('/api/check', { method: 'POST' });
            } catch (e) {
                console.error('Failed to trigger check:', e);
            }
        }

        // ========================================
        // Initialize
        // ========================================
        init();
    </script>
</body>
</html>
`
