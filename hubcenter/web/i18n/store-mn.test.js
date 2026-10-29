'use strict';

var fs = require('fs');
var path = require('path');
var failed = 0;
var passed = 0;

function assert(cond, message) {
  if (cond) {
    passed += 1;
    console.log('  OK ' + message);
    return;
  }
  failed += 1;
  console.log('  ERR ' + message);
}

var pages = [
  {
    name: 'home',
    file: path.join(__dirname, '..', 'home', 'index.html'),
    en: 'Hub Center · MaClaw deployment hub and control plane',
    mn: 'Hub Center · MaClaw байршуулалтын төв ба удирдлагын хавтгай',
    zh: 'Hub Center · MaClaw 部署中枢与控制平面'
  },
  {
    name: 'petstore',
    file: path.join(__dirname, '..', 'petstore', 'index.html'),
    en: 'MaClaw Pet Market',
    mn: 'MaClaw тэжээвэр амьтны зах зээл',
    zh: 'MaClaw 宠物市场'
  },
  {
    name: 'expertmarket',
    file: path.join(__dirname, '..', 'expertmarket', 'index.html'),
    en: 'MaClaw AI Expert Market',
    mn: 'MaClaw AI шинжээчийн зах зээл',
    zh: 'MaClaw AI 专家市场'
  }
];

pages.forEach(function (page) {
  var src = fs.readFileSync(page.file, 'utf8');
  console.log('\n=== ' + page.name + ' ===\n');
  assert(src.indexOf('Монгол') !== -1, page.name + ' contains Монгол');
  assert(src.indexOf('HubCenterI18n') !== -1, page.name + ' contains HubCenterI18n');
  assert(/[А-Яа-яӨөҮү]/.test(src), page.name + ' contains a Cyrillic UI string');
  assert(src.indexOf(page.en) !== -1, page.name + ' has an English title');
  assert(src.indexOf(page.mn) !== -1, page.name + ' has a Mongolian title');
  assert(src.indexOf(page.zh) !== -1, page.name + ' keeps the Chinese title');
  assert(src.indexOf('.htmlLang(') !== -1, page.name + ' sets documentElement lang via htmlLang');
  var langJs = src.indexOf('<script src="/i18n/lang.js"></script>');
  var inline = src.lastIndexOf('<script>');
  assert(langJs !== -1 && inline > langJs, page.name + ' loads /i18n/lang.js before the inline script');
});

var pet = fs.readFileSync(pages[1].file, 'utf8');
assert(pet.indexOf('new URLSearchParams(location.search)') !== -1, 'petstore reads location.search');
assert(pet.indexOf("params.get('lang')") !== -1, 'petstore reads the lang query');
assert(pet.indexOf('normalizeHubLang(queryLang)') !== -1, 'petstore does not ignore the lang query');
assert(pet.indexOf('hubcenter_petstore_lang') !== -1, 'petstore has its own localStorage key');
assert(pet.indexOf('Could not load the market') !== -1 && pet.indexOf('Зах зээлийг ачаалж чадсангүй') !== -1, '无法加载市场 has en and mn');
assert(pet.indexOf('Request failed') !== -1 && pet.indexOf('Хүсэлт амжилтгүй') !== -1, '请求失败 has en and mn');
assert(pet.indexOf('Previous page') !== -1 && pet.indexOf('Өмнөх хуудас') !== -1, '上一页 has en and mn');
assert(pet.indexOf('Next page') !== -1 && pet.indexOf('Дараах хуудас') !== -1, '下一页 has en and mn');
assert(pet.indexOf('Confirm purchase') !== -1 && pet.indexOf('Худалдан авалтыг батлах') !== -1, '确认购买 has en and mn');
assert(pet.indexOf("en:'Free'") !== -1 && pet.indexOf("mn:'Үнэгүй'") !== -1, '免费 has en and mn');
assert(pet.indexOf("en:'Buy out'") !== -1 && pet.indexOf("mn:'Бүрмөсөн авах'") !== -1, '买断 has en and mn');

var expert = fs.readFileSync(pages[2].file, 'utf8');
assert(expert.indexOf("params.get('lang')") !== -1, 'expertmarket reads the lang query');
assert(expert.indexOf('normalizeHubLang(queryLang)') !== -1, 'expertmarket does not ignore the lang query');
assert(expert.indexOf('hubcenter_expertmarket_lang') !== -1, 'expertmarket has its own localStorage key');
assert(expert.indexOf('Request failed. Try again later.') !== -1, 'expertmarket request fallback has English');

var home = fs.readFileSync(pages[0].file, 'utf8');
assert(home.indexOf('hubcenter_home_lang') !== -1, 'home has its own localStorage key');
assert(home.indexOf('detectHubLang(navigator.languages, saved)') !== -1, 'home lets a saved language win, else navigator.languages');

console.log('\n' + passed + ' passed, ' + failed + ' failed');
if (failed) process.exit(1);
