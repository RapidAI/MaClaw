'use strict';
var fs = require('fs');
var files = [
  'hubcenter/web/skillmarket/index.html',
  'hubcenter/web/skillmarket/user/index.html',
  'hubcenter/web/skillhub/index.html',
  'hubcenter/web/gossip/index.html',
  'hubcenter/web/developer/index.html',
  'hubcenter/web/developer/docs/index.html'
];
var re = /\{\s*zh\s*:\s*('(?:\\'|[^'])*'|"(?:\\"|[^"])*")\s*,\s*en\s*:\s*('(?:\\'|[^'])*'|"(?:\\"|[^"])*")\s*\}/g;
var pairRe = /\[('(?:\\'|[^'])*'|"(?:\\"|[^"])*")\s*,\s*('(?:\\'|[^'])*'|"(?:\\"|[^"])*")\]/g;
files.forEach(function(f) {
  var s = fs.readFileSync(f, 'utf8');
  var n = 0;
  console.log('\n## ' + f);
  var m;
  var rx = /\{\s*zh\s*:/.test(s) ? re : pairRe;
  if (f.indexOf('developer') >= 0) rx = pairRe;
  while ((m = rx.exec(s))) {
    n++;
    var zh = eval(m[1]);
    var en = eval(m[2]);
    console.log(JSON.stringify(en) + '\t' + JSON.stringify(zh));
  }
  console.log('count ' + n);
});
