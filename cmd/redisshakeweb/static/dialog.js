let feedbackDialog;
let previousFocus;

function createFeedbackDialog() {
  const dialog = document.createElement('dialog');
  dialog.className = 'feedback-dialog';
  dialog.setAttribute('aria-labelledby', 'feedback-dialog-title');
  dialog.setAttribute('aria-describedby', 'feedback-dialog-message');
  dialog.innerHTML = `<div class="feedback-dialog-content">
    <span class="feedback-dialog-icon" aria-hidden="true"></span>
    <h2 id="feedback-dialog-title"></h2>
    <p id="feedback-dialog-message"></p>
  </div>
  <div class="feedback-dialog-actions"><button type="button">知道了</button></div>`;
  dialog.querySelector('button').addEventListener('click', () => dialog.close());
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
  feedbackDialog.querySelector('button').focus();
}

export const showErrorDialog = message => showFeedbackDialog(message, true);
export const showInfoDialog = message => showFeedbackDialog(message, false);
