let openControl = null;

function closeMenu(focusTrigger = false) {
  if (!openControl) return;
  const control = openControl;
  openControl = null;
  control.classList.remove('select-open', 'select-opens-up');
  control.querySelector('.select-menu').hidden = true;
  const trigger = control.querySelector('.select-trigger');
  trigger.setAttribute('aria-expanded', 'false');
  if (focusTrigger) trigger.focus();
}

function openMenu(control) {
  closeMenu();
  const menu = control.querySelector('.select-menu');
  if (!menu.children.length) return;
  menu.hidden = false;
  control.classList.add('select-open');
  control.classList.toggle('select-opens-up', window.innerHeight - control.getBoundingClientRect().bottom < Math.min(menu.scrollHeight + 8, 270) && control.getBoundingClientRect().top > window.innerHeight - control.getBoundingClientRect().bottom);
  control.querySelector('.select-trigger').setAttribute('aria-expanded', 'true');
  openControl = control;
  (menu.querySelector('[aria-selected="true"]') || menu.querySelector('[role="option"]'))?.focus();
}

function moveOption(menu, step) {
  const options = [...menu.querySelectorAll('[role="option"]:not(:disabled)')];
  const current = options.indexOf(document.activeElement);
  const next = options[(current + step + options.length) % options.length];
  next?.focus();
}

export function syncSelect(select) {
  const control = select.closest('.select-control');
  if (!control) return;
  const menu = control.querySelector('.select-menu');
  const trigger = control.querySelector('.select-trigger');
  const selected = select.selectedOptions[0];
  const label = selected?.textContent || '暂无运行记录';
  control.querySelector('.select-value').textContent = label;
  trigger.setAttribute('aria-label', `${control.dataset.label}：${label}`);
  trigger.disabled = !select.options.length || select.disabled;
  const options = [...select.options];
  const signature = JSON.stringify(options.map(option => [option.value, option.textContent, option.disabled]));
  if (menu.dataset.options === signature) {
    [...menu.children].forEach((item, index) => item.setAttribute('aria-selected', String(options[index] === selected)));
    return;
  }
  menu.dataset.options = signature;
  menu.replaceChildren();
  options.forEach(option => {
    const item = document.createElement('button');
    item.type = 'button';
    item.className = 'select-option';
    item.setAttribute('role', 'option');
    item.setAttribute('aria-selected', String(option === selected));
    item.tabIndex = -1;
    item.disabled = option.disabled;
    item.textContent = option.textContent;
    item.addEventListener('click', () => {
      if (select.value !== option.value) {
        select.value = option.value;
        select.dispatchEvent(new Event('change', { bubbles: true }));
      }
      syncSelect(select);
      closeMenu(true);
    });
    menu.appendChild(item);
  });
}

export function enhanceSelects(container) {
  if (openControl && !openControl.isConnected) openControl = null;
  container.querySelectorAll('select:not(.select-native)').forEach(select => {
    const control = document.createElement('div');
    control.className = 'select-control';
    if (select.style.minWidth) control.style.minWidth = select.style.minWidth;
    const label = container.querySelector(`label[for="${CSS.escape(select.id)}"]`);
    control.dataset.label = label?.textContent || select.getAttribute('aria-label') || '选择选项';
    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'select-trigger';
    trigger.id = `${select.id}-trigger`;
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    trigger.setAttribute('aria-controls', `${select.id}-menu`);
    trigger.innerHTML = '<span class="select-value"></span><svg viewBox="0 0 20 20" aria-hidden="true"><path d="m5 7 5 5 5-5"/></svg>';
    const menu = document.createElement('div');
    menu.className = 'select-menu';
    menu.id = `${select.id}-menu`;
    menu.setAttribute('role', 'listbox');
    menu.setAttribute('aria-label', control.dataset.label);
    menu.hidden = true;
    select.before(control);
    control.append(select, trigger, menu);
    select.classList.add('select-native');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');
    if (label) label.htmlFor = trigger.id;
    trigger.addEventListener('click', () => openControl === control ? closeMenu() : openMenu(control));
    trigger.addEventListener('keydown', event => {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault();
        openMenu(control);
      }
    });
    menu.addEventListener('keydown', event => {
      if (event.key === 'Escape') { event.preventDefault(); closeMenu(true); }
      else if (event.key === 'Tab') closeMenu();
      else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault();
        moveOption(menu, event.key === 'ArrowDown' ? 1 : -1);
      } else if (event.key === 'Home' || event.key === 'End') {
        event.preventDefault();
        const options = [...menu.querySelectorAll('[role="option"]:not(:disabled)')];
        (event.key === 'Home' ? options[0] : options.at(-1))?.focus();
      }
    });
    select.addEventListener('change', () => syncSelect(select));
    syncSelect(select);
  });
}

document.addEventListener('pointerdown', event => {
  if (openControl && !openControl.contains(event.target)) closeMenu();
});
