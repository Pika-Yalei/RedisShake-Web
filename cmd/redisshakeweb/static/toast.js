let dismissCurrent;

export function dismissToast() {
  dismissCurrent?.();
}

function showToast(message, error) {
  dismissToast();
  const previousFocus = document.activeElement;
  const toast = document.createElement('aside');
  toast.className = 'feedback-toast';
  toast.dataset.tone = error ? 'error' : 'success';
  toast.setAttribute('aria-label', error ? '错误提示' : '操作提示');
  toast.innerHTML = `<svg class="feedback-toast-icon" viewBox="0 0 20 20" aria-hidden="true">
    <circle cx="10" cy="10" r="8"/>
    <path d="${error ? 'm7 7 6 6m0-6-6 6' : 'm6 10 3 3 5-6'}"/>
  </svg>
  <p class="feedback-toast-message" role="${error ? 'alert' : 'status'}" aria-atomic="true"></p>
  <button type="button" class="feedback-toast-close" aria-label="关闭提示">
    <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m3.5 3.5 9 9m0-9-9 9"/></svg>
  </button>`;

  let timer;
  let remaining = 4500;
  let startedAt;
  const dismiss = () => {
    clearTimeout(timer);
    const restoreFocus = toast.contains(document.activeElement);
    toast.remove();
    if (dismissCurrent === dismiss) dismissCurrent = null;
    if (restoreFocus && previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
  };
  const pause = () => {
    if (!timer) return;
    clearTimeout(timer);
    timer = null;
    remaining -= Date.now() - startedAt;
  };
  const resume = () => {
    if (error || timer || !toast.isConnected || toast.matches(':hover') || toast.contains(document.activeElement)) return;
    startedAt = Date.now();
    timer = setTimeout(dismiss, Math.max(0, remaining));
  };
  dismissCurrent = dismiss;
  toast.querySelector('button').addEventListener('click', dismiss);
  toast.addEventListener('mouseenter', pause);
  toast.addEventListener('mouseleave', resume);
  toast.addEventListener('focusin', pause);
  toast.addEventListener('focusout', () => queueMicrotask(resume));
  document.body.append(toast);
  toast.querySelector('p').textContent = String(message || (error ? '操作失败，请重试。' : '操作已完成。'));
  resume();
}

export const showErrorToast = message => showToast(message, true);
export const showSuccessToast = message => showToast(message, false);
