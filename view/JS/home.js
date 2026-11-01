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

  // Keep keyboard focus within the expanded mobile navigation and its controls.
  document.querySelector('.site-header').addEventListener('keydown', event => {
    if (event.key !== 'Tab' || toggle.getAttribute('aria-expanded') !== 'true') return;
    const controls = [...document.querySelectorAll('.site-header a, .site-header button')]
      .filter(el => el.getClientRects().length);
    const first = controls[0], last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  const progress = document.querySelector('.scroll-progress');
  function updateProgress() {
    const distance = document.documentElement.scrollHeight - innerHeight;
    progress.style.transform = `scaleX(${distance > 0 ? scrollY / distance : 0})`;
    document.querySelector('.site-header').classList.toggle('is-scrolled', scrollY > 12);
  }
  addEventListener('scroll', updateProgress, { passive: true });
  addEventListener('resize', updateProgress);
  new ResizeObserver(updateProgress).observe(document.body);
  updateProgress();

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
