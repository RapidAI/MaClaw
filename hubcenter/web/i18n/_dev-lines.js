'use strict';
var fs = require('fs');
['hubcenter/web/developer/index.html', 'hubcenter/web/developer/docs/index.html'].forEach(function(f) {
  var lines = fs.readFileSync(f, 'utf8').split(/\n/);
  console.log('\n## ' + f + ' lines ' + lines.length);
  lines.forEach(function(line, i) {
    if (/langButton|applyLanguage|function apply|browserLanguage|savedLanguage|data-hub-lang|<script/.test(line)) {
      console.log((i + 1) + ' len ' + line.length + ' ' + line.slice(0, 180));
    }
  });
});
