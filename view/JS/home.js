document.addEventListener('DOMContentLoaded', () => {
  const toggle = document.querySelector('.menu-toggle');
  const nav = document.getElementById('main-nav');
  const navLinks = [...nav.querySelectorAll('.nav-item')];

  function setMenu(open) {
    toggle.setAttribute('aria-expanded', String(open));
    toggle.setAttribute('aria-label', open ? 'Close navigation' : 'Open navigation');
    toggle.title = open ? 'Close navigation' : 'Open navigation';
    toggle.firstElementChild.className = open ? 'fas fa-xmark' : 'fas fa-bars';
    nav.classList.toggle('is-open', open);
  }

  toggle.addEventListener('click', () => setMenu(toggle.getAttribute('aria-expanded') !== 'true'));
  navLinks.forEach(link => link.addEventListener('click', () => setMenu(false)));
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape' && toggle.getAttribute('aria-expanded') === 'true') {
      setMenu(false);
      toggle.focus();
    }
  });
  document.addEventListener('click', event => {
    if (!event.target.closest('.site-header')) setMenu(false);
  });
  matchMedia('(min-width: 761px)').addEventListener('change', () => setMenu(false));

  const observer = new IntersectionObserver(entries => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      navLinks.forEach(link => {
        if (link.hash === '#' + entry.target.id) link.setAttribute('aria-current', 'location');
        else link.removeAttribute('aria-current');
      });
    }
  }, { rootMargin: '-15% 0px -60% 0px' });
  navLinks.forEach(link => {
    if (!link.hash) return;
    const section = document.querySelector(link.hash);
    if (section) observer.observe(section);
  });

  ['History', 'Details'].forEach(name => {
    const button = document.getElementById('toggle' + name);
    const panel = document.getElementById(name.toLowerCase() + 'Table');
    // Existing statistics code controls visibility; observe its actual state.
    new MutationObserver(() => {
      button.setAttribute('aria-expanded', String(panel.style.display !== 'none'));
    }).observe(panel, { attributes: true, attributeFilter: ['style'] });
  });
});
