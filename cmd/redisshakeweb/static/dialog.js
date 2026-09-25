let feedbackDialog;
let previousFocus;

function createFeedbackDialog() {
  const dialog = document.createElement('dialog');
  dialog.className = 'feedback-dialog';
  dialog.setAttribute('aria-labelledby', 'feedback-dialog-title');
  dialog.setAttribute('aria-describedby', 'feedback-dialog-message');
  dialog.innerHTML = `<div class="feedback-dialog-content">
    <button type="button" class="feedback-dialog-close" aria-label="关闭提示">
      <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m3.5 3.5 9 9m0-9-9 9"/></svg>
    </button>
    <span class="feedback-dialog-icon" aria-hidden="true"></span>
    <h2 id="feedback-dialog-title"></h2>
    <p id="feedback-dialog-message"></p>
  </div>
  <div class="feedback-dialog-actions"><button type="button">知道了</button></div>`;
  dialog.querySelector('.feedback-dialog-close').addEventListener('click', () => dialog.close());
  dialog.querySelector('.feedback-dialog-actions button').addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => {
    if (previousFocus?.isConnected) previousFocus.focus();
    else document.querySelector('#app input')?.focus();
    previousFocus = null;
  });
  document.body.append(dialog);
  return dialog;
}

function showFeedbackDialog(message, error) {
  feedbackDialog ??= createFeedbackDialog();
  feedbackDialog.dataset.tone = error ? 'error' : 'info';
  feedbackDialog.setAttribute('role', error ? 'alertdialog' : 'dialog');
  feedbackDialog.querySelector('.feedback-dialog-icon').textContent = error ? '!' : 'i';
  feedbackDialog.querySelector('#feedback-dialog-title').textContent = error ? '操作未完成' : '提示';
  feedbackDialog.querySelector('#feedback-dialog-message').textContent = String(message || (error ? '操作失败，请重试。' : '操作已完成。'));
  if (!feedbackDialog.open) {
    previousFocus = document.activeElement;
    feedbackDialog.showModal();
  }
  feedbackDialog.querySelector('.feedback-dialog-actions button').focus();
}

export const showErrorDialog = message => showFeedbackDialog(message, true);
export const showInfoDialog = message => showFeedbackDialog(message, false);
