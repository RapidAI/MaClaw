// Polyfill DOM methods not implemented in jsdom.
Element.prototype.scrollIntoView = Element.prototype.scrollIntoView || function () {};
