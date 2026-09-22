import { formatHardConfig } from './settings-state';

const configList = document.getElementById('configList');
if (!configList) {
  throw new Error('Missing required element: configList');
}

for (const entry of formatHardConfig()) {
  const row = document.createElement('div');
  row.className = 'config-row';

  const label = document.createElement('div');
  label.className = 'config-label';
  label.textContent = entry.label;

  const value = document.createElement('div');
  value.className = 'config-value';
  value.textContent = entry.value;

  row.append(label, value);
  configList.appendChild(row);
}
