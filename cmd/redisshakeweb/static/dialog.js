let errorDialog;
let previousFocus;

function createErrorDialog() {
  const dialog = document.createElement('dialog');
  dialog.className = 'error-dialog';
  dialog.setAttribute('role', 'alertdialog');
  dialog.setAttribute('aria-labelledby', 'error-dialog-title');
  dialog.setAttribute('aria-describedby', 'error-dialog-message');
  dialog.innerHTML = `<div class="error-dialog-content">
    <span class="error-dialog-icon" aria-hidden="true">!</span>
    <h2 id="error-dialog-title">操作未完成</h2>
    <p id="error-dialog-message"></p>
    <div class="error-dialog-actions"><button type="button" class="primary">知道了</button></div>
  </div>`;
  dialog.querySelector('button').addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => {
    if (previousFocus?.isConnected) previousFocus.focus();
    previousFocus = null;
  });
  document.body.append(dialog);
  return dialog;
}

export function showErrorDialog(message) {
  errorDialog ??= createErrorDialog();
  errorDialog.querySelector('#error-dialog-message').textContent = String(message || '操作失败，请重试。');
  if (!errorDialog.open) {
    previousFocus = document.activeElement;
    errorDialog.showModal();
  }
  errorDialog.querySelector('button').focus();
}
