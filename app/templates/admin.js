let editor;
const PHASE_LABELS = {
    1: { name: '测活', countLabel: '存活' },
    2: { name: '流媒体+重命名', countLabel: '完成' },
    3: { name: '测速', countLabel: '通过' }
};

// 缓存高频访问的 DOM 元素
const dom = {
    apiKey: document.getElementById('apiKey'),
    toggleApiKey: document.getElementById('toggleApiKey'),
    saveApiKey: document.getElementById('saveApiKey'),
    logs: document.getElementById('logs'),
    phaseSteps: document.getElementById('phaseSteps'),
    progressText: document.getElementById('progressText'),
    progressPercent: document.getElementById('progressPercent'),
    processPercent: document.getElementById('processPercent'),
    progressBarTotal: document.getElementById('progressBarTotal'),
    progressBarSuccess: document.getElementById('progressBarSuccess'),
    successLabel: document.getElementById('successLabel'),
    successText: document.getElementById('successText'),
    statusContainer: document.getElementById('statusContainer'),
    statusIcon: document.getElementById('statusIcon'),
    statusText: document.getElementById('nextCheckTime'),
    versionInfo: document.getElementById('versionInfo'),
    openConfigModal: document.getElementById('openConfigModal'),
    exportConfig: document.getElementById('exportConfig'),
    importConfig: document.getElementById('importConfig'),
    configFileInput: document.getElementById('configFileInput'),
    saveConfigForm: document.getElementById('saveConfigForm'),
    reloadConfigForm: document.getElementById('reloadConfigForm')
};

// 初始化API密钥
const storedApiKey = localStorage.getItem('apiKey') || '';
dom.apiKey.value = storedApiKey;

// 切换API密钥可见性
dom.toggleApiKey.addEventListener('click', function() {
    const icon = this.querySelector('i');

    if (dom.apiKey.type === 'password') {
        dom.apiKey.type = 'text';
        icon.className = 'bi bi-eye';
    } else {
        dom.apiKey.type = 'password';
        icon.className = 'bi bi-eye-slash';
    }
});

// 打开配置模态框
dom.openConfigModal.addEventListener('click', function(e) {
    e.preventDefault(); // 阻止 Bootstrap 的默认行为
    loadConfigForm().then(() => {
        const modal = new bootstrap.Modal(document.getElementById('configModal'));
        modal.show();
    });
});

// 导出配置文件
dom.exportConfig.addEventListener('click', function() {
    exportConfigFile();
});

// 导入配置文件
dom.importConfig.addEventListener('click', function() {
    dom.configFileInput.click();
});

dom.configFileInput.addEventListener('change', function(event) {
    const file = event.target.files && event.target.files[0];
    if (!file) return;
    const valid = /\.(ya?ml)$/i.test(file.name);
    if (!valid) {
        showAlertMessage('请选择 YAML 格式的配置文件', 'warning');
        dom.configFileInput.value = '';
        return;
    }

    const reader = new FileReader();
    reader.onload = function(e) {
        const content = e.target.result;
        if (typeof content !== 'string') {
            showAlertMessage('读取配置文件失败', 'danger');
            return;
        }
        importConfigFile(content);
    };

    reader.onerror = function() {
        showAlertMessage('读取配置文件失败', 'danger');
    };
    reader.readAsText(file, 'utf-8');
    dom.configFileInput.value = '';
});

// 保存配置表单
dom.saveConfigForm.addEventListener('click', function() {
    saveConfigForm();
});

// 刷新配置表单
dom.reloadConfigForm.addEventListener('click', function() {
    loadConfigFormWrapper();
});

// 保存配置表单
function saveConfigForm() {
    const button = dom.saveConfigForm;
    const originalText = button.textContent;
    button.disabled = true;
    button.textContent = '保存中...';

    fetch('/api/config/form', {
        method: 'POST',
        headers: addApiKeyHeader({ 'Content-Type': 'application/json' }),
        body: JSON.stringify(serializeFormData())
    })
    .then(response => {
        if (handleUnauthorized(response, true)) throw new Error('未授权');
        return response.json();
    })
    .then(data => {
        if (data.error) {
            showAlertMessage('配置保存失败: ' + data.error, 'danger');
        } else {
            showAlertMessage(data.message, 'success');
            updateStatus();
            loadConfig();
            loadConfigForm();
        }
    })
    .catch(error => {
        if (error.message !== '未授权') {
            showAlertMessage('配置保存失败: ' + error.message, 'danger');
        }
    })
    .finally(() => {
        button.disabled = false;
        button.textContent = originalText;
    });
}

// 刷新配置表单
function loadConfigFormWrapper() {
    const button = dom.reloadConfigForm;
    const icon = button.querySelector('i');
    icon.classList.add('rotate-animation');
    button.disabled = true;
    loadConfigForm().finally(() => {
        setTimeout(() => { icon.classList.remove('rotate-animation'); button.disabled = false; }, 500);
    });
}

// 保存API密钥到本地存储
dom.saveApiKey.addEventListener('click', function() {
    const apiKey = dom.apiKey.value.trim();
    localStorage.setItem('apiKey', apiKey);

    const button = this;
    const originalText = button.textContent;
    button.disabled = true;
    button.textContent = '验证中...';

    fetch('/api/status', {
        headers: { 'X-API-Key': apiKey }
    })
    .then(response => {
        if (response.status === 401) {
            alert('API密钥无效，请检查后重试');
            return false;
        } else if (response.ok) {
            alert('API密钥有效，已保存到本地');
            loadConfig();
            loadLogs();
            updateStatus();
            return true;
        } else {
            throw new Error('验证失败');
        }
    })
    .catch(error => {
        console.error('验证API密钥失败:', error);
        alert('验证API密钥时出错，但已保存到本地存储');
    })
    .finally(() => {
        button.disabled = false;
        button.textContent = originalText;
    });
});

// 获取API密钥
function getApiKey() {
    return localStorage.getItem('apiKey') || '';
}

// 添加API密钥到请求头
function addApiKeyHeader(headers = {}) {
    const apiKey = getApiKey();
    return { ...headers, 'X-API-Key': apiKey };
}

// 初始化编辑器
require(['vs/editor/editor.main'], function() {
    monaco.languages.register({ id: 'yaml' });
    monaco.languages.setMonarchTokensProvider('yaml', {
        tokenizer: {
            root: [
                [/^[\s]*#.*$/, 'comment'],
                [/^[\s]*[\w\-]+:/, 'keyword'],
                [/^[\s]*-/, 'keyword'],
                [/".*?"/, 'string'],
                [/'.*?'/, 'string'],
            ]
        }
    });

    editor = monaco.editor.create(document.getElementById('editor'), {
        language: 'yaml',
        theme: 'vs',
        automaticLayout: true,
        minimap: { enabled: false },
        fontSize: 13,
        lineHeight: 20,
        padding: { top: 10 },
        scrollBeyondLastLine: false,
        renderLineHighlight: 'gutter',
    });

    loadConfig();

    const toggleConfigBtn = document.getElementById('toggleConfig');
    const editorOverlay = document.getElementById('editorOverlay');

    editorOverlay.style.display = 'flex';

    toggleConfigBtn.addEventListener('click', function() {
        const isHidden = editorOverlay.style.display === 'flex';
        editorOverlay.style.display = isHidden ? 'none' : 'flex';
        const icon = toggleConfigBtn.querySelector('i');
        icon.className = isHidden ? 'bi bi-eye' : 'bi bi-eye-slash';
    });

    editorOverlay.addEventListener('click', function() {
        editorOverlay.style.display = 'none';
        const icon = toggleConfigBtn.querySelector('i');
        icon.className = 'bi bi-eye';
    });
});

// 处理未授权响应
function handleUnauthorized(response, showAlert = true) {
    if (response.status === 401) {
        if (showAlert) {
            showAlertMessage('API密钥无效或未提供，请检查您的API密钥', 'danger');
        }
        return true;
    }
    return false;
}

function showAlertMessage(message, type = 'info') {
    const alertBanner = document.getElementById('alertBanner');
    alertBanner.className = 'alert alert-' + type;
    alertBanner.textContent = message;
    alertBanner.style.display = 'block';
    setTimeout(() => {
        alertBanner.style.display = 'none';
    }, 5000);
}

// 加载配置
function loadConfig() {
    return fetch('/api/config', {
        headers: addApiKeyHeader()
    })
    .then(response => {
        if (handleUnauthorized(response, false)) return;
        if (!response.ok) throw new Error('加载配置失败');
        return response.json();
    })
    .then(data => {
        if (editor && data) editor.setValue(data.content);
        return data;
    })
    .catch(error => {
        console.error('加载配置失败:', error);
        showAlertMessage('加载配置失败: ' + error.message, 'danger');
    });
}

function fieldId(field) {
    return 'field-' + field.yamlKey.replace(/\./g, '-');
}

function buildFormField(field, value) {
    const id = fieldId(field);
    const label = `<label class="form-label" for="${id}">${field.label}</label>`;
    const help = field.help ? `<div class="form-text text-muted">${field.help}</div>` : '';
    let input = '';

    if (field.type === 'checkbox') {
        input = `
            <div class="form-check form-switch">
                <input class="form-check-input" type="checkbox" id="${id}">
                <label class="form-check-label" for="${id}">${field.label}</label>
            </div>
            ${help}`;
        return `<div class="col-12 mt-2">${input}</div>`;
    }

    if (field.type === 'select') {
        const options = field.options.map(option => `
                <option value="${escapeHtml(option)}"${value === option ? ' selected' : ''}>${escapeHtml(option)}</option>`).join('');
        input = `<select id="${id}" class="form-select">${options}</select>`;
    } else if (field.type === 'textarea') {
        const content = value != null && Array.isArray(value) ? value.join('\n') : (value != null ? String(value) : '');
        input = `<textarea id="${id}" class="form-control" rows="${field.rows || 4}" placeholder="${escapeHtml(field.placeholder || '')}">${escapeHtml(content)}</textarea>`;
    } else {
        input = `<input id="${id}" class="form-control" type="${field.type}" value="${escapeHtml(value != null ? String(value) : '')}" ${field.step ? `step="${field.step}"` : ''} placeholder="${escapeHtml(field.placeholder || '')}">`;
    }

    return `
        <div class="col-md-6">
            ${label}
            ${input}
            ${help}
        </div>`;
}

function getConfigValue(config, yamlKey) {
    if (!config) return null;
    const parts = yamlKey.split('.');
    let value = config;
    for (const part of parts) {
        if (value == null || typeof value !== 'object' || !(part in value)) {
            return null;
        }
        value = value[part];
    }
    return value;
}

function setFieldValue(field, value) {
    const el = document.getElementById(fieldId(field));
    if (!el) return;

    if (field.type === 'checkbox') {
        el.checked = Boolean(value);
        return;
    }

    if (field.type === 'textarea') {
        if (Array.isArray(value)) {
            el.value = value.join('\n');
        } else if (value != null) {
            el.value = String(value);
        } else {
            el.value = '';
        }
        return;
    }

    el.value = value != null ? String(value) : '';
}

function renderConfigForm(formData) {
    const container = document.getElementById('configFormContainer');
    if (!container) return;
    container.innerHTML = '';

    // 按section分组字段
    const sections = {};
    window.CONFIG_FORM_FIELDS.forEach(field => {
        if (!sections[field.section]) {
            sections[field.section] = [];
        }
        sections[field.section].push(field);
    });

    // 创建accordion
    const accordionId = 'configAccordion';
    let accordionHtml = `<div class="accordion" id="${accordionId}">`;

    let index = 0;
    for (const [sectionName, fields] of Object.entries(sections)) {
        const collapseId = `collapse${index}`;
        const headingId = `heading${index}`;
        const isExpanded = index === 0 ? 'show' : ''; // 默认展开第一个
        const expandedClass = index === 0 ? '' : 'collapsed';

        accordionHtml += `
            <div class="accordion-item">
                <h2 class="accordion-header" id="${headingId}">
                    <button class="accordion-button ${expandedClass}" type="button" data-bs-toggle="collapse" data-bs-target="#${collapseId}" aria-expanded="${index === 0 ? 'true' : 'false'}" aria-controls="${collapseId}">
                        <i class="bi bi-gear me-2"></i>${sectionName}
                    </button>
                </h2>
                <div id="${collapseId}" class="accordion-collapse collapse ${isExpanded}" aria-labelledby="${headingId}" data-bs-parent="#${accordionId}">
                    <div class="accordion-body">
                        <div class="row g-3">
        `;

        fields.forEach(field => {
            const value = getConfigValue(formData.config, field.yamlKey);
            accordionHtml += buildFormField(field, value);
        });

        accordionHtml += `
                        </div>
                    </div>
                </div>
            </div>
        `;
        index++;
    }

    accordionHtml += '</div>';
    container.insertAdjacentHTML('beforeend', accordionHtml);

    // 设置字段值
    window.CONFIG_FORM_FIELDS.forEach(field => {
        const value = getConfigValue(formData.config, field.yamlKey);
        setFieldValue(field, value);
    });
}

function loadConfigForm() {
    return fetch('/api/config/form', {
        headers: addApiKeyHeader()
    })
    .then(response => {
        if (handleUnauthorized(response, false)) return;
        if (!response.ok) throw new Error('加载配置表单失败');
        return response.json();
    })
    .then(data => {
        if (!data) return;
        renderConfigForm(data);
        return data;
    })
    .catch(error => {
        console.error('加载配置表单失败:', error);
        showAlertMessage('加载配置表单失败: ' + error.message, 'danger');
    });
}

function setNestedValue(obj, key, value) {
    const parts = key.split('.');
    let cursor = obj;
    for (let i = 0; i < parts.length - 1; i++) {
        const part = parts[i];
        if (typeof cursor[part] !== 'object' || cursor[part] === null) {
            cursor[part] = {};
        }
        cursor = cursor[part];
    }
    cursor[parts[parts.length - 1]] = value;
}

function serializeFormData() {
    const payload = {};
    window.CONFIG_FORM_FIELDS.forEach(field => {
        const el = document.getElementById(fieldId(field));
        if (!el) return;

        let value;
        if (field.type === 'checkbox') {
            value = el.checked;
        } else if (field.type === 'textarea') {
            const text = el.value.trim();
            value = text ? text.split('\n').map(line => line.trim()).filter(Boolean) : [];
        } else if (field.type === 'number') {
            const raw = el.value.trim();
            if (raw === '') return;
            value = Number(raw);
            if (Number.isNaN(value)) return;
        } else {
            value = el.value;
        }

        setNestedValue(payload, field.yamlKey, value);
    });
    return payload;
}

function exportConfigFile() {
    fetch('/api/config', {
        headers: addApiKeyHeader()
    })
    .then(response => {
        if (handleUnauthorized(response, true)) throw new Error('未授权');
        if (!response.ok) throw new Error('导出失败');
        return response.json();
    })
    .then(data => {
        const blob = new Blob([data.content], { type: 'text/yaml;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = 'config.yaml';
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(url);
        showAlertMessage('配置已导出为 config.yaml', 'success');
    })
    .catch(error => {
        if (error.message !== '未授权') {
            showAlertMessage('导出配置失败: ' + error.message, 'danger');
        }
    });
}

function importConfigFile(content) {
    fetch('/api/config', {
        method: 'POST',
        headers: addApiKeyHeader({ 'Content-Type': 'application/json' }),
        body: JSON.stringify({ content })
    })
    .then(response => {
        if (handleUnauthorized(response, true)) throw new Error('未授权');
        return response.json();
    })
    .then(data => {
        if (data.error) {
            showAlertMessage('导入配置失败: ' + data.error, 'danger');
        } else {
            showAlertMessage('配置已导入并保存', 'success');
            updateStatus();
            loadConfig();
            loadConfigForm();
        }
    })
    .catch(error => {
        if (error.message !== '未授权') {
            showAlertMessage('导入配置失败: ' + error.message, 'danger');
        }
    });
}

// 保存配置
const saveConfigEl = document.getElementById('saveConfig');
if (saveConfigEl) {
    saveConfigEl.addEventListener('click', function() {
        const content = editor.getValue();

        fetch('/api/config', {
            method: 'POST',
            headers: addApiKeyHeader({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ content })
        })
        .then(response => {
            if (handleUnauthorized(response, true)) throw new Error('未授权');
            return response.json();
        })
        .then(data => {
            if (data.error) {
                showAlertMessage('保存失败: ' + data.error, 'danger');
            } else {
                showAlertMessage(data.message, 'success');
                updateStatus();
                loadConfig();
            }
        })
        .catch(error => {
            if (error.message !== '未授权') {
                showAlertMessage('保存失败: ' + error.message, 'danger');
            }
        });
    });
}

// 重新加载配置
const reloadConfigEl = document.getElementById('reloadConfig');
if (reloadConfigEl) {
    reloadConfigEl.addEventListener('click', function() {
        const button = this;
        const icon = button.querySelector('i');
        icon.classList.add('rotate-animation');
        button.disabled = true;
        loadConfig().finally(() => {
            setTimeout(() => { icon.classList.remove('rotate-animation'); button.disabled = false; }, 500);
        });
    });
}

// 强制关闭
const forceCloseEl = document.getElementById('forceClose');
if (forceCloseEl) {
    forceCloseEl.addEventListener('click', function() {
        if (!confirm('确定要强制停止当前阶段吗？已有结果将被保留。')) return;

        const button = this;
        const icon = button.querySelector('i');
        const originalIcon = icon.className;
        icon.className = 'bi bi-arrow-repeat me-1 rotate-animation';
        button.disabled = true;

        fetch('/api/force-close', {
            method: 'POST',
            headers: addApiKeyHeader()
        })
        .then(response => {
            if (handleUnauthorized(response, true)) throw new Error('未授权');
            return response.json();
        })
        .catch(error => {
            if (error.message !== '未授权') {
                console.error('强制停止失败:', error);
                alert('强制停止失败: ' + error.message);
            }
        })
        .finally(() => {
            setTimeout(() => { icon.className = originalIcon; button.disabled = false; }, 500);
        });
    });
}

// 立即检测
const triggerCheckEl = document.getElementById('triggerCheck');
if (triggerCheckEl) {
    triggerCheckEl.addEventListener('click', function() {
        const button = this;
        const icon = button.querySelector('i');
        const originalIcon = icon.className;
        icon.className = 'bi bi-arrow-repeat me-1 rotate-animation';
        button.disabled = true;

        fetch('/api/trigger-check', {
            method: 'POST',
            headers: addApiKeyHeader()
        })
        .then(response => {
            if (handleUnauthorized(response, true)) throw new Error('未授权');
            return response.json();
        })
        .then(data => { loadLogs(); })
        .catch(error => {
            if (error.message !== '未授权') console.error('触发检测失败:', error);
        })
        .finally(() => {
            setTimeout(() => { icon.className = originalIcon; button.disabled = false; }, 500);
        });
    });
}

// 加载日志
function loadLogs() {
    return fetch('/api/logs', {
        headers: addApiKeyHeader()
    })
    .then(response => {
        if (handleUnauthorized(response, false)) throw new Error('未授权');
        return response.json();
    })
    .then(data => {
        if (data.error) {
            dom.logs.textContent = '加载日志失败: ' + data.error;
        } else {
            const isScrolledToBottom = dom.logs.scrollHeight - dom.logs.clientHeight <= dom.logs.scrollTop + 1;
            const fragment = document.createDocumentFragment();
            data.logs.forEach(log => {
                const logLine = document.createElement('div');
                logLine.innerHTML = colorizeLog(log);
                fragment.appendChild(logLine);
            });
            dom.logs.innerHTML = '';
            dom.logs.appendChild(fragment);
            if (isScrolledToBottom) {
                dom.logs.scrollTop = dom.logs.scrollHeight;
            }
        }
        return data;
    })
    .catch(error => {
        if (error.message !== '未授权') {
            dom.logs.textContent = '加载日志失败: ' + error.message;
        }
    });
}

function escapeHtml(str) {
    return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

// 为日志添加颜色
const LOG_LEVEL_CLASS = { INF: 'log-info', ERR: 'log-error', WRN: 'log-warn', DBG: 'log-debug' };
function colorizeLog(log) {
    log = escapeHtml(log);
    log = log.replace(/^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})/, '<span class="log-time">$1</span>');
    log = log.replace(/\b(INF|ERR|WRN|DBG)\b/, (m) => '<span class="' + LOG_LEVEL_CLASS[m] + '">' + m + '</span>');
    return log;
}

// 刷新日志
const refreshLogsEl = document.getElementById('refreshLogs');
if (refreshLogsEl) {
    refreshLogsEl.addEventListener('click', function() {
        const button = this;
        const icon = button.querySelector('i');
        icon.classList.add('rotate-animation');
        button.disabled = true;
        loadLogs().finally(() => {
            setTimeout(() => { icon.classList.remove('rotate-animation'); button.disabled = false; }, 500);
        });
    });
}

// 缓存阶段步骤 DOM 元素
let phaseStepEls = [null, null, null, null];
let phaseCountEls = [null, null, null, null];

function initPhaseElements() {
    phaseStepEls = [null, document.getElementById('phaseStep1'), document.getElementById('phaseStep2'), document.getElementById('phaseStep3')];
    phaseCountEls = [null, document.getElementById('phaseCount1'), document.getElementById('phaseCount2'), document.getElementById('phaseCount3')];
}

initPhaseElements();

// 更新阶段步骤指示器
function updatePhaseSteps(phase, available, total, phaseResults) {
    // 检查是否有任何阶段结果
    const hasResults = phaseResults && (phaseResults['1'] || phaseResults['2'] || phaseResults['3']);

    if (phase === 0 && !hasResults) {
        dom.phaseSteps.style.display = 'none';
        return;
    }

    dom.phaseSteps.style.display = 'flex';

    for (let i = 1; i <= 3; i++) {
        const step = phaseStepEls[i];
        const count = phaseCountEls[i];
        const label = PHASE_LABELS[i] ? PHASE_LABELS[i].countLabel : '';
        const result = phaseResults ? phaseResults[String(i)] : null;

        step.classList.remove('active', 'done');

        if (i < phase) {
            // 已完成的阶段：用后端保存的结果
            step.classList.add('done');
            if (result) {
                count.textContent = label + ': ' + result.available + '/' + result.total;
            }
        } else if (i === phase) {
            // 当前阶段：用实时数据
            step.classList.add('active');
            count.textContent = label + ': ' + available + '/' + total;
        } else if (phase === 0 && result) {
            // 空闲时：显示上次结果
            step.classList.add('done');
            count.textContent = label + ': ' + result.available + '/' + result.total;
        } else {
            count.textContent = '';
        }
    }
}

// 更新进度条
function updateProgressBar(total, processed, available, phase, phaseResults) {
    const percentTotal = total > 0 ? (processed / total * 100).toFixed(1) : 0;

    dom.progressText.textContent = processed + '/' + total;
    dom.progressPercent.textContent = percentTotal + '%';
    dom.progressBarTotal.style.width = percentTotal + '%';

    const percentSuccess = total > 0 ? (available / total * 100).toFixed(1) : 0;
    dom.processPercent.textContent = percentSuccess + '%';

    // 显示阶段对应的标签
    const label = (phase && PHASE_LABELS[phase]) ? PHASE_LABELS[phase].countLabel : '成功节点';
    if (dom.successLabel && dom.successText) {
        dom.successLabel.childNodes[0].textContent = label + ': ';
        if (available > 0) {
            dom.successText.innerHTML = '<span class="success-highlight">' + available + '</span>/' + total;
        } else {
            dom.successText.textContent = available + '/' + total;
        }
    }

    dom.progressBarSuccess.style.width = percentSuccess + '%';

    // 更新阶段指示器
    updatePhaseSteps(phase, available, total, phaseResults);
}

// 重置进度条到初始状态
function resetProgress() {
    dom.progressText.textContent = 'N/A';
    dom.progressPercent.textContent = 'N/A';
    dom.processPercent.textContent = 'N/A';
    dom.progressBarTotal.style.width = '0%';
    dom.progressBarSuccess.style.width = '0%';
    if (dom.successLabel) {
        dom.successLabel.childNodes[0].textContent = '成功节点: ';
        dom.successText.textContent = 'N/A';
    }
}

// 更新状态
// Pipeline stages run concurrently, so rows stay "active" with live counts.
// `done` counts items that finished the stage; `pass` counts items that moved on.
// When speed testing is off, the speed row stays visible but inactive with an
// empty counter — matching the legacy "grayed, no data" look rather than
// hiding the row outright (which shifted the layout).
function updatePipelineSteps(p, hasSpeed) {
    if (!dom.phaseSteps) return;
    initPhaseElements();
    if (!phaseStepEls || !phaseCountEls) return;

    dom.phaseSteps.style.display = 'flex';
    const rows = [
        { idx: 1, label: '存活', pass: p.alivePass,  total: p.total, active: true },
        { idx: 2, label: '通过', pass: p.filterPass, total: p.alivePass, active: true },
        { idx: 3, label: '通过', pass: p.speedPass, total: p.filterPass, active: hasSpeed },
    ];
    for (const r of rows) {
        const step = phaseStepEls[r.idx];
        const count = phaseCountEls[r.idx];
        if (!step || !count) continue;
        step.classList.remove('active', 'done');
        if (r.active) {
            step.classList.add('active');
            count.textContent = r.label + ': ' + r.pass + '/' + r.total;
        } else {
            count.textContent = '';
        }
    }
}

function updatePipelineBar(p, hasSpeed) {
    const total = p.total;
    const processed = p.aliveDone;                         // how far through input
    const success = hasSpeed ? p.speedPass : p.filterPass; // end-to-end pass count

    const pctProcessed = total > 0 ? (processed / total * 100).toFixed(1) : 0;
    dom.progressText.textContent = processed + '/' + total;
    dom.progressPercent.textContent = pctProcessed + '%';
    dom.progressBarTotal.style.width = pctProcessed + '%';

    const pctSuccess = total > 0 ? (success / total * 100).toFixed(1) : 0;
    dom.processPercent.textContent = pctSuccess + '%';
    dom.progressBarSuccess.style.width = pctSuccess + '%';

    if (dom.successLabel && dom.successText) {
        dom.successLabel.childNodes[0].textContent = '通过: ';
        dom.successText.innerHTML = success > 0
            ? '<span class="success-highlight">' + success + '</span>/' + total
            : success + '/' + total;
    }
}

function updateStatus() {
    fetch('/api/status', {
        headers: addApiKeyHeader()
    })
    .then(response => {
        if (handleUnauthorized(response, false)) return;
        return response.json();
    })
    .then(data => {
        if (data) {
            if (data.checking) {
                dom.statusContainer.className = 'text-primary';
                dom.statusIcon.className = 'bi bi-arrow-repeat me-1 rotate-animation';
                dom.statusText.textContent = '正在检测中...';

                if (data.pipeline) {
                    updatePipelineBar(data.pipeline, !!data.hasSpeedTest);
                    updatePipelineSteps(data.pipeline, !!data.hasSpeedTest);
                } else {
                    // legacy fallback (older server)
                    updateProgressBar(data.proxyCount, data.progress, data.available, data.phase, data.phaseResults);
                }
            } else {
                dom.statusContainer.className = 'text-success';
                dom.statusIcon.className = 'bi bi-check-circle me-1';
                dom.statusText.textContent = '空闲';

                updatePhaseSteps(0, 0, 0, data.phaseResults);
                resetProgress();
            }
        }
    })
    .catch(error => {
        console.error('获取状态失败:', error);
        dom.statusContainer.className = 'text-danger';
        dom.statusIcon.className = 'bi bi-exclamation-triangle me-1';
        dom.statusText.textContent = '获取状态失败';
        resetProgress();
    });
}

// 初始加载
updateStatus();
loadLogs();
loadConfigForm();

// 检查API密钥
const apiKey = localStorage.getItem('apiKey');
if (!apiKey) {
    alert('请先输入API密钥以使用本系统');
    dom.apiKey.focus();
}

// 定时刷新
setInterval(loadLogs, 10000);
setInterval(updateStatus, 3000);

// 获取版本信息
function getVersionInfo() {
    fetch('/api/version', {
        headers: addApiKeyHeader()
    })
    .then(response => response.json())
    .then(data => {
        if (data && data.version) {
            dom.versionInfo.textContent = '版本: ' + data.version;
        }
    })
    .catch(error => {
        console.error('获取版本信息失败:', error);
        dom.versionInfo.textContent = '版本: 未知';
    });
}

getVersionInfo();