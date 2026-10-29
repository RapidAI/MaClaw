'use strict';
var fs = require('fs');
var objBare = /\{\s*zh\s*:\s*('(?:\\'|[^'])*'|"(?:\\"|[^"])*")\s*,\s*en\s*:\s*('(?:\\'|[^'])*'|"(?:\\"|[^"])*")\s*\}/g;
var files = [
  'hubcenter/web/skillmarket/index.html',
  'hubcenter/web/skillmarket/user/index.html',
  'hubcenter/web/skillhub/index.html',
  'hubcenter/web/gossip/index.html',
  'hubcenter/web/developer/index.html',
  'hubcenter/web/developer/docs/index.html'
];
files.forEach(function(f) {
  var s = fs.readFileSync(f, 'utf8');
  var bare = (s.match(objBare) || []).length;
  var mn = (s.match(/\bmn\s*:/g) || []).length;
  console.log(f, 'bare', bare, 'mn', mn, 'cyr', /[А-Яа-яӨөҮү]/.test(s), 'btn', s.indexOf("English', '中文") >= 0 || s.indexOf("English','中文") >= 0);
});
var dev = fs.readFileSync('hubcenter/web/developer/docs/index.html', 'utf8');
var i = dev.indexOf('#communityLink');
console.log(dev.slice(i, i + 400));
var sm = fs.readFileSync('hubcenter/web/skillmarket/index.html', 'utf8');
var j = sm.indexOf('title:{');
console.log(sm.slice(j, j + 280));
