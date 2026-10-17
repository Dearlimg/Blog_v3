document.addEventListener('DOMContentLoaded', () => {
  const form = document.getElementById('messageForm');
  const list = document.getElementById('messagesList');
  const refresh = document.getElementById('refreshMessages');
  const loadMore = document.getElementById('loadMoreMessages');
  const submit = form.querySelector('.submit-btn');
  const pageSize = 20;
  let loading = false;
  let currentPage = 1;

  function element(tag, className, text) {
    const node = document.createElement(tag);
    node.className = className;
    node.textContent = text;
    return node;
  }

  function createMessage(message) {
    const item = element('article', 'message-item', '');
    const header = element('div', 'message-header', '');
    const date = new Date(message.create_at);
    const formattedDate = Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
    header.append(element('span', 'message-author', message.name || 'Anonymous'));
    const time = element('time', 'message-date', formattedDate);
    if (formattedDate) time.dateTime = date.toISOString();
    header.append(time);
    item.append(header, element('p', 'message-content', message.content || ''));
    const meta = element('div', 'message-meta', '');
    if (message.email) meta.append(element('span', 'meta-email', message.email));
    if (message.ip) meta.append(element('span', 'meta-ip', 'IP ' + message.ip));
    item.append(meta);
    return item;
  }

  async function loadMessages(append = false) {
    if (loading) return;
    loading = true;
    refresh.disabled = true;
    loadMore.disabled = true;
    list.setAttribute('aria-busy', 'true');
    const requestedPage = append ? currentPage + 1 : 1;
    if (append) {
      loadMore.textContent = 'Loading...';
    } else {
      loadMore.hidden = true;
      list.replaceChildren(element('div', 'message-loading', 'Loading messages...'));
    }
    try {
      const response = await API.get(`/messages?page=${requestedPage}&page_size=${pageSize}`);
      if (!Array.isArray(response.data) || typeof response.total !== 'number') throw new Error('Invalid message response');
      const messages = response.data.map(createMessage);
      if (append) {
        list.querySelectorAll('.message-error').forEach(error => error.remove());
        list.append(...messages);
      } else {
        list.replaceChildren(...messages);
      }
      currentPage = requestedPage;
      loadMore.hidden = currentPage * response.page_size >= response.total;
      if (!append && !messages.length) list.append(element('div', 'no-messages', 'A quiet corner for a conversation. Leave the first note.'));
    } catch {
      const errorNotice = element('div', 'message-error', 'Messages are unavailable right now.');
      const retry = element('button', 'retry-messages', 'Try again');
      retry.id = 'retry-messages';
      retry.type = 'button';
      retry.addEventListener('click', () => loadMessages(append));
      errorNotice.append(retry);
      if (append) list.append(errorNotice);
      else list.replaceChildren(errorNotice);
    } finally {
      loading = false;
      refresh.disabled = false;
      loadMore.disabled = false;
      loadMore.textContent = 'Load older messages';
      list.setAttribute('aria-busy', 'false');
    }
  }

  function notify(text, type) {
    form.parentElement.querySelector('.form-notification')?.remove();
    const notice = element('div', 'form-notification message-' + type, text);
    notice.setAttribute('role', type === 'error' ? 'alert' : 'status');
    form.before(notice);
  }

  refresh.addEventListener('click', () => loadMessages(false));
  loadMore.addEventListener('click', () => loadMessages(true));
  form.addEventListener('submit', async event => {
    event.preventDefault();
    if (submit.disabled) return;
    const data = new FormData(form);
    const content = String(data.get('message') || '').trim();
    if (!content) {
      notify('Please write a message before sending.', 'error');
      form.elements.message.focus();
      return;
    }
    const original = submit.innerHTML;
    submit.disabled = true;
    submit.textContent = 'Sending...';
    try {
      await API.post('/messages', {
        name: String(data.get('name') || '').trim(),
        email: String(data.get('email') || '').trim(),
        content,
      });
      form.reset();
      notify('Thank you. Your message has been sent.', 'success');
      await loadMessages();
    } catch (error) {
      const message = error.message === 'please wait before submitting another message'
        ? 'Please wait one minute before posting again.'
        : error.message === 'message submission is temporarily unavailable'
          ? 'Message submission is temporarily unavailable. Please try again shortly.'
          : 'Your message could not be sent. Please check the fields and try again.';
      notify(message, 'error');
    } finally {
      submit.innerHTML = original;
      submit.disabled = false;
    }
  });
  loadMessages();
});
