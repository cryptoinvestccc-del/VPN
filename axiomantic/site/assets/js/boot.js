/* Runs before the first paint (it is the one blocking script, 300 bytes):
   turns on the CSS intro unless the visitor asked for reduced motion. */
(function (html) {
  var mq = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)');
  if (!mq || !mq.matches) html.className += ' motion';
})(document.documentElement);
