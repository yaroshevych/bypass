export function installDialogDismissal(dialogs, dismiss) {
  for (const dialog of dialogs) {
    dialog.addEventListener('click', event => {
      const rect = dialog.getBoundingClientRect();
      if (event.target.closest('.overlay-close') || (event.target === dialog &&
        (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom))) {
        dismiss(dialog);
      }
    });
  }
}
