import { localizeText, mnExactSentence, normalizeLang } from './langSelect';

export const appsMnByEnglish: Record<string, string> = {
  'Search apps': 'Апп хайх',
  'Clear search': 'Хайлтыг цэвэрлэх',
  'Reset filters': 'Шүүлтүүрийг дахин тохируулах',
  'Create': 'Үүсгэх',
  'Category': 'Ангилал',
  'Actions': 'Үйлдэл',
  'Approval status': 'Баталгааны төлөв',
  'Open approval instance management': 'Баталгааны тохиолдлын удирдлагыг нээх',
  'Run history': 'Гүйлтийн түүх',
  'Review app execution history': 'Аппын гүйцэтгэлийн түүхийг харах',
  'App Market': 'App зах зээл',
  'Search and install apps': 'Апп хайж суулгах',
  'All apps': 'Бүх апп',
  'Other apps': 'Бусад апп',
  'Pinned apps': 'Түгээмэл апп',
  'Recent': 'Сүүлийн',
  'Search results': 'Хайлтын үр дүн',
  'Never used': 'Ашиглаагүй',
  'Status': 'Төлөв',
  'Event': 'Үйл явдал',
  'Health': 'Эрүүл байдал',
  'Available': 'Боломжтой',
  'Opened': 'Нээгдсэн',
  'Running': 'Ажиллаж байна',
  'Disabled': 'Идэвхгүй',
  'Disabled by enterprise policy. The entry and history are retained.': 'Байгууллагын бодлогоор идэвхгүй болгосон. Орох цэг ба түүх хадгалагдсан.',
  'Deprecated by the enterprise capability market. The entry and history are retained.': 'Байгууллагын чадавхын зах зээл энэ аппын шинэ суулгалт эсвэл ажиллуулалтыг зогсоосон. Орох цэг ба түүх хадгалагдсан.',
  'Revoked by the enterprise capability market. The entry and history are retained.': 'Байгууллагын чадавхын зах зээл энэ аппыг буцаасан. Орох цэг ба түүх хадгалагдсан.',
  'Create, manage, and add apps from the capability market.': 'Апп үүсгэх, удирдах, чадавхын зах зээлээс нэмэх.',
  'Create app': 'Апп үүсгэх',
  'Generate draft from chat': 'Чатаас ноорог үүсгэх',
  'Generate draft': 'Ноорог үүсгэх',
  'Example: build a contract filing app, upload Word/PDF, output archive number and review result': 'Жишээ: гэрээ архивлах апп хийж, Word/PDF байршуулан, архивын дугаар ба хяналтын үр дүнг гаргана',
  'Manage apps': 'Апп удирдах',
  'Review / publish': 'Хянах / нийтлэх',
  'Check whether local apps are ready for upload to the enterprise capability market.': 'Локал аппыг байгууллагын чадавхын зах зээлд байршуулахад бэлэн эсэхийг шалгана.',
  'Publish checklist': 'Нийтлэх шалгалтын жагсаалт',
  'Submission package preview': 'Илгээх багцын урьдчилсан харагдац',
  'Only apps that pass every checklist item are included; incomplete apps stay out of the package.': 'Шалгах жагсаалтын зүйл бүрийг давсан апп л орно; дутуу апп багцад орохгүй.',
  'Ready to submit': 'Илгээхэд бэлэн',
  'Needs work': 'Нөхөх шаардлагатай',
  'No local apps ready for publishing yet': 'Нийтлэхэд бэлэн локал апп одоогоор алга',
  'Submit for review': 'Хянуулахаар илгээх',
  'Submitted': 'Илгээсэн',
  'Waiting for enterprise market review': 'Байгууллагын зах зээлийн хяналтыг хүлээж байна',
  'Local sync pending': 'Локал синк хүлээгдэж байна',
  'Modified locally, resubmit required': 'Локалаар өөрчилсөн, дахин илгээх шаардлагатай',
  'Local change': 'Локал өөрчлөлт',
  'Submitting': 'Илгээж байна',
  'Enterprise market is not connected yet; saved as a local pending submission.': 'Байгууллагын зах зээл одоогоор холбогдоогүй; локал хүлээгдэж буй илгээлтээр хадгалсан.',
  'Local submission queue': 'Локал илгээлтийн дараалал',
  'No local pending submissions': 'Локал хүлээгдэж буй илгээлт алга',
  'Failed to read submission queue': 'Илгээлтийн дарааллыг уншиж чадсангүй',
  'Reading submission queue': 'Илгээлтийн дарааллыг уншиж байна',
  'Review evidence': 'Хяналтын нотолгоо',
  'Install governance': 'Суулгалтын удирдлага',
  'Runtime evidence': 'Ажиллах үеийн нотолгоо',
  'Package signature': 'Багцын гарын үсэг',
  'Approval': 'Баталгаа',
  'Progress': 'Явц',
  'Workflow': 'Ажлын урсгал',
  'Refresh': 'Шинэчлэх',
  'Refreshing': 'Шинэчилж байна',
  'Sync to Hub': 'Hub руу синк хийх',
  'One-click publish': 'Нэг товчоор нийтлэх',
  'Publishing...': 'Нийтлэж байна...',
  'Local queue + Enterprise Hub + SkillMarket/HubCenter (per config)': 'Локал дараалал + байгууллагын Hub + SkillMarket/HubCenter (тохиргоогоор)',
  'Remote publish readiness': 'Алсын нийтлэх бэлэн байдал',
  'Remote targets ready': 'Алсын зорилт бэлэн',
  'Remote targets may partially fail': 'Алсын зорилт хэсэгчлэн амжилтгүй байж болно',
  'Publish blocked': 'Нийтлэл хаагдсан',
  'Checking remote readiness…': 'Алсын бэлэн байдлыг шалгаж байна…',
  'Remote preflight unavailable': 'Алсын урьдчилсан шалгалт боломжгүй',
  'Local queue': 'Локал дараалал',
  'SkillMarket': 'SkillMarket',
  'Enterprise Hub pack': 'Байгууллагын Hub багц',
  'Can publish; remote targets may be partial': 'Нийтэлж болно; алсын зорилт хэсэгчилсэн байж болно',
  'Local queue will succeed; SkillMarket or Hub pack may fail. You can retry afterwards.': 'Локал дараалал амжилттай болно; SkillMarket эсвэл Hub багц амжилтгүй байж болно. Дараа нь дахин оролдож болно.',
  'One-click publish (may partial)': 'Нэг товчоор нийтлэх (хэсэгчилсэн байж болно)',
  'Syncing': 'Синк хийж байна',
  'Refresh Hub Status': 'Hub төлөвийг шинэчлэх',
  'Hub sync failed': 'Hub синк амжилтгүй',
  'One-click publish partially failed': 'Нэг товчоор нийтлэх хэсэгчлэн амжилтгүй',
  'Install approved app': 'Зөвшөөрөгдсөн аппыг суулгах',
  'Installing': 'Суулгаж байна',
  'Installed': 'Суулгасан',
  'Install failed': 'Суулгалт амжилтгүй',
  'Last refreshed': 'Сүүлд шинэчилсэн',
  'Copy queued package': 'Дарааллын багцыг хуулах',
  'Copy audit': 'Аудит хуулах',
  'View details': 'Дэлгэрэнгүй харах',
  'Hide details': 'Дэлгэрэнгүйг хураах',
  'Reading details': 'Дэлгэрэнгүйг уншиж байна',
  'Submission details': 'Илгээлтийн дэлгэрэнгүй',
  'Package apps': 'Багцын апп',
  'Audit events': 'Аудитын үйл явдал',
  'Copying': 'Хуулж байна',
  'Copied': 'Хуулсан',
  'Audit copied': 'Аудит хуулсан',
  'Full package unavailable': 'Бүтэн багцыг унших боломжгүй',
  'Review issues': 'Хяналтын асуудал',
  'plus': 'өөр',
  'more': 'зүйл',
  'Fix': 'Засах',
  'Resolve dependencies': 'Хамаарлыг шийдвэрлэх',
  'Resolving dependencies…': 'Хамаарлыг шийдвэрлэж байна…',
  'Dependencies verified; evidence updated': 'Хамаарал баталгаажсан; нотолгоо шинэчлэгдсэн',
  'Dependencies ready; collecting run evidence from the test protocol…': 'Хамаарал бэлэн; тестийн протоколоос гүйлтийн нотолгоо цуглуулж байна…',
  'Dependencies and run evidence are ready': 'Хамаарал ба гүйлтийн нотолгоо бэлэн',
  'Dependencies ready; run the app once for evidence (test protocol may need a file input)': 'Хамаарал бэлэн; нотолгоо авахад аппыг нэг удаа ажиллуулна уу (тестийн протоколд файлын оролт хэрэгтэй байж болно)',
  'Dependencies ready; auto-run finished but evidence failed publish checks (coverage/fingerprint). Adjust the test protocol and run again': 'Хамаарал бэлэн; автомат гүйлт дууссан боловч нотолгоо нийтлэх шалгалтыг даваагүй (хамрах хүрээ/хурууны хээ). Тестийн протоколыг засаад дахин ажиллуулна уу',
  'Dependencies ready, but automatic run-evidence collection failed': 'Хамаарал бэлэн боловч гүйлтийн нотолгоог автоматаар цуглуулж чадсангүй',
  'timed out': 'Хугацаа дууссан',
  'empty run id': 'Хоосон гүйлтийн ID',
  'Version': 'Хувилбар',
  'Reviewer': 'Хянагч',
  'Risk': 'Эрсдэл',
  'Approved scopes': 'Зөвшөөрсөн хамрах хүрээ',
  'Events': 'Үйл явдал',
  'Changes requested': 'Засвар хүссэн',
  'Approved': 'Зөвшөөрсөн',
  'Published': 'Нийтэлсэн',
  'Deprecated': 'Шинэ суулгалт зогссон',
  'Revoked': 'Буцаагдсан',
  'Submission ID': 'Илгээлтийн ID',
  'Withdraw submission': 'Илгээлтийг буцаах',
  'Run': 'Ажиллуулах',
  'Run again': 'Дахин ажиллуулах',
  'Reset': 'Дахин тохируулах',
  'Output': 'Гаралт',
  'Drop or choose files': 'Файл чирэх эсвэл сонгох',
  'Choose file': 'Файл сонгох',
  'Selected': 'Сонгосон',
  'No file selected': 'Файл сонгоогүй',
  'Waiting to run': 'Ажиллуулахыг хүлээж байна',
  'Output generated': 'Гаралт үүссэн',
  'Skill run started': 'Skill эхэлсэн',
  'Skill running': 'Skill ажиллаж байна',
  'Skill completed': 'Skill дууссан',
  'Skill failed': 'Skill амжилтгүй',
  'Skill cancelled': 'Skill цуцлагдсан',
  'The running session is no longer available. The app may have restarted or the background task was cleaned up. Review history or run it again.': 'Ажиллаж буй сесс хүчингүй болсон. Апп дахин эхэлсэн эсвэл арын даалгаврыг цэвэрлэсэн байж болно. Түүхийг харах эсвэл дахин ажиллуулна уу.',
  'Run steps': 'Гүйлтийн алхам',
  'Output artifacts': 'Гаралтын бүтээл',
  'Input': 'Оролт',
  'Output files or text results appear here after execution.': 'Гүйцэтгэсний дараа гаралтын файл эсвэл текстийн үр дүн энд гарна.',
  'Run completed': 'Гүйлт дууссан',
  'Pin to favorites': 'Түгээмэл болгох',
  'Remove from favorites': 'Түгээмэлээс хасах',
  'Waiting for artifact': 'Бүтээл хүлээж байна',
  'Artifact generated': 'Бүтээл үүссэн',
  'Open': 'Нээх',
  'Download and open': 'Татаад нээх',
  'Reveal': 'Хавтаст харуулах',
  'Steps and artifact status appear after execution': 'Гүйцэтгэсний дараа алхам ба бүтээлийн төлөв гарна',
  'File is larger than 25MB and cannot be uploaded this way yet': 'Файл 25MB-аас том тул энэ аргаар одоогоор байршуулах боломжгүй',
  'Cancel run': 'Гүйлтийг болих',
  'Run details': 'Гүйлтийн дэлгэрэнгүй',
  'Aggregated execution history saved on this device.': 'Энэ төхөөрөмжид хадгалсан гүйцэтгэлийн түүхийг нэгтгэн харуулна.',
  'No app run history yet': 'Аппын гүйлтийн түүх одоогоор алга',
  'Approval instances': 'Баталгааны тохиолдол',
  'Approval instance management': 'Баталгааны тохиолдлын удирдлага',
  'Requests, pending work, and outcomes from every approval app are managed here.': 'Баталгааны апп бүрийн хүсэлт, хүлээгдэж буй ажил, үр дүнг энд удирдана.',
  'Local approval instances': 'Локал баталгааны тохиолдол',
  'Select an instance on the left to inspect details and available actions on the right.': 'Дэлгэрэнгүй ба боломжит үйлдлийг баруун талд харахын тулд зүүнээс тохиолдол сонгоно уу.',
  'Approval instance details': 'Баталгааны тохиолдлын дэлгэрэнгүй',
  'After selecting an instance on the left, this area shows timeline, results, and approval actions.': 'Зүүнээс тохиолдол сонгосны дараа энд замнал, үр дүн, баталгааны үйлдэл гарна.',
  'Search approvals': 'Баталгаа хайх',
  'App': 'App',
  'All statuses': 'Бүх төлөв',
  'Loading approval instances': 'Баталгааны тохиолдлыг ачаалж байна',
  'Failed to load approval instances': 'Баталгааны тохиолдлыг ачаалж чадсангүй',
  'Recent instances': 'Сүүлийн тохиолдол',
  'Pending': 'Хүлээгдэж буй',
  'View all approval status': 'Бүх баталгааны төлөвийг харах',
  'View this app approvals': 'Энэ аппын баталгааг харах',
  'My requests': 'Миний хүсэлт',
  'Pending my approval': 'Миний батлахыг хүлээж байна',
  'Handled': 'Шийдвэрлэсэн',
  'Rejected': 'Татгалзсан',
  'Needs attention': 'Анхаарал хэрэгтэй',
  'All': 'Бүгд',
  'Document outputs': 'Баримтын гаралт',
  'Text outputs': 'Текстийн гаралт',
  'DataSrv approval overview': 'DataSrv баталгааны тойм',
  'Aggregates my requests, approvals, decisions, and output types from DataSrv app-installations.': 'DataSrv app-installations-аас миний хүсэлт, баталгаа, шийдвэр, гаралтын төрлийг нэгтгэнэ.',
  'DataSrv is disabled; showing local approval instances only.': 'DataSrv идэвхгүй; зөвхөн локал баталгааны тохиолдлыг харуулж байна.',
  'Aggregating DataSrv approval results': 'DataSrv баталгааны үр дүнг нэгтгэж байна',
  'Failed to load the DataSrv approval overview': 'DataSrv баталгааны тоймыг ачаалж чадсангүй',
  'No matching approval apps yet': 'Тохирох баталгааны апп одоогоор алга',
  'DataSrv approval details': 'DataSrv баталгааны дэлгэрэнгүй',
  'Open approval': 'Баталгааг нээх',
  'Open record': 'Бичлэгийг нээх',
  'Instance data': 'Тохиолдлын өгөгдөл',
  'Current node': 'Одоогийн зангилаа',
  'Node status': 'Зангилааны төлөв',
  'Node tasks': 'Зангилааны даалгавар',
  'Applicant': 'Хүсэлт гаргагч',
  'Approver': 'Батлагч',
  'Current assignee': 'Одоогийн хариуцагч',
  'Assignee type': 'Хариуцагчийн төрөл',
  'Status transition': 'Төлөвийн шилжилт',
  'Approval timeline': 'Баталгааны замнал',
  'This is the detail and action area. Select an approval instance on the left first.': 'Энэ бол дэлгэрэнгүй ба үйлдлийн хэсэг. Эхлээд зүүнээс баталгааны тохиолдол сонгоно уу.',
  'Approval actions': 'Баталгааны үйлдэл',
  'Approve': 'Зөвшөөрөх',
  'Reject': 'Татгалзах',
  'Mark attention': 'Анхааралд авах',
  'Continue with supplement': 'Нэмэлтээр үргэлжлүүлэх',
  'Supplement note': 'Нэмэлт тайлбар',
  'Material or attachment reference': 'Материал эсвэл хавсралтын лавлагаа',
  'Missing items': 'Дутуу зүйл',
  'Continuing workflow': 'Ажлын урсгалыг үргэлжлүүлж байна',
  'Approval workflow': 'Баталгааны ажлын урсгал',
  'Business record': 'Бизнесийн бичлэг',
  'Business object': 'Бизнесийн объект',
  'Remote approval': 'Алсын баталгаа',
  'Business status': 'Бизнесийн төлөв',
  'Result status': 'Үр дүнгийн төлөв',
  'View full workflow': 'Бүтэн ажлын урсгалыг харах',
  'No runs yet': 'Гүйлт одоогоор алга',
  'No approval instances in this lane': 'Энэ ангилалд баталгааны тохиолдол алга',
  'Clear history': 'Түүхийг цэвэрлэх',
  'Choose an app': 'Апп сонгох',
  'Click an app icon on the left to open the app.': 'Аппыг нээхийн тулд зүүн талын аппын дүрс дээр дарна уу.',
  'No matching apps': 'Тохирох апп алга',
  'No more apps': 'Өөр апп алга',
  'Pin': 'Бэхлэх',
  'Unpin': 'Салгах',
  'Pinned apps are full at 8. Unpin one first.': 'Түгээмэл апп 8-аар дүүрсэн. Эхлээд нэгийг нь салгана уу.',
  'Edit': 'Засах',
  'Duplicate app': 'Апп хуулбарлах',
  'Copy': 'Хуулах',
  'Save': 'Хадгалах',
  'Cancel': 'Болих',
  'Move up': 'Дээш зөөх',
  'Move down': 'Доош зөөх',
  'Move to top': 'Дээд рүү зөөх',
  'Move to bottom': 'Доод руу зөөх',
  'Up': 'Дээш',
  'Down': 'Доош',
  'Top': 'Дээд',
  'Bottom': 'Доод',
  'Clear filters before sorting': 'Эрэмбэлэхийн өмнө шүүлтүүрийг цэвэрлэнэ үү',
  'Hide': 'Нуух',
  'Remove': 'Хасах',
  'Disable': 'Идэвхгүй болгох',
  'Enable': 'Идэвхжүүлэх',
  'Restore': 'Сэргээх',
  'Hidden apps': 'Нуугдсан апп',
  'DataSrv discovery': 'DataSrv чадавх илрүүлэлт',
  'Connected': 'Холбогдсон',
  'Loading': 'Ачаалж байна',
  'Unavailable': 'Боломжгүй',
  'Add to panel': 'Самбарт нэмэх',
  'Added': 'Нэмсэн',
  'Adding': 'Нэмж байна',
  'In panel': 'Самбарт байна',
  'Discovered apps': 'Үүсгэж болох апп',
  'Found apps': 'Олдсон апп',
  'Could not check installed capabilities': 'Суулгасан чадавхыг шалгаж чадсангүй',
  'Current draft manifest': 'Одоогийн ноорог манифест',
  'Live JSON for review and copy, synchronized from the configuration above': 'Дээрх тохиргооноос синк хийгдсэн, хянах ба хуулахад зориулсан шууд JSON',
  'Manifest': 'Манифест',
  'Copy app pack': 'Аппын багцыг хуулах',
  'Fields': 'Талбар',
  'Add field': 'Талбар нэмэх',
  'Delete field': 'Талбар устгах',
  'Field name': 'Талбарын нэр',
  'Label': 'Харуулах нэр',
  'Type': 'Төрөл',
  'Required': 'Заавал',
  'Default': 'Анхдагч',
  'Options': 'Сонголт',
  'Icon color': 'Дүрсний өнгө',
  'Complete required input': 'Шаардлагатай оролтыг гүйцээнэ үү',
  'Install app package': 'Аппын багц суулгах',
  'App catalog': 'App жагсаалт',
  'Import app package': 'Аппын багц импортлох',
  'Paste reviewed app package JSON for admin installs or private apps.': 'Админы суулгалт эсвэл хувийн аппад зориулж хянагдсан аппын багцын JSON-г буулгана уу.',
  'Source': 'Эх сурвалж',
  'Addable': 'Нэмэх боломжтой',
  'Upgradable': 'Шинэчлэх боломжтой',
  'Search enterprise Hub apps': 'Байгууллагын Hub апп хайх',
  'Search Hub': 'Hub хайх',
  'Searching': 'Хайж байна',
  'No apps found': 'Апп олдсонгүй',
  'Search failed': 'Хайлт амжилтгүй',
  'Add': 'Нэмэх',
  'Paste app package JSON (maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json)': 'Аппын багцын JSON буулгах (maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json)',
  'Install': 'Суулгах',
  'Confirm install': 'Суулгалтыг баталгаажуулах',
  'Selected upgrades include high-risk new permissions; confirm once more.': 'Сонгосон шинэчлэлд өндөр эрсдэлтэй шинэ зөвшөөрөл багтсан; дахин нэг удаа баталгаажуулна уу.',
  'Checking Skill dependencies': 'Skill хамаарлыг шалгаж байна',
  'Skill dependencies ready': 'Skill хамаарал бэлэн',
  'Install dependencies and run': 'Хамаарлыг суулгаад ажиллуулах',
  'Installing dependencies': 'Хамаарлыг суулгаж байна',
  'Required Skill dependencies are missing or unavailable. Install or enable them first.': 'Шаардлагатай Skill хамаарал дутуу эсвэл боломжгүй. Эхлээд суулгах эсвэл идэвхжүүлнэ үү.',
  'Dependency check failed': 'Хамаарлын шалгалт амжилтгүй',
  'Dependency verification': 'Хамаарлын баталгаажуулалт',
  'Dependency diagnostics': 'Хамаарлын оношлогоо',
  'Install trace': 'Суулгалтын мөр',
  'Preflight': 'Урьдчилсан шалгалт',
  'Resolve': 'Шийдвэрлэх',
  'Download': 'Татах',
  'Integrity': 'Бүрэн бүтэн байдал',
  'Install step': 'Суулгах алхам',
  'Error': 'Алдаа',
  'Dependency verification complete': 'Хамаарлын баталгаажуулалт дууссан',
  'Dependency verification found blocking items': 'Хамаарлын баталгаажуулалтаар саатуулах зүйл олдсон',
  'Version snapshot': 'Хувилбарын агшин',
  'Workspace layout': 'Ажлын талбарын бүтэц',
  'Result contract': 'Үр дүнгийн гэрээ',
  'Result coverage': 'Үр дүнгийн хамрагдалт',
  'Covered': 'Хамрагдсан',
  'Missing': 'Дутуу',
  'Test evidence': 'Тестийн нотолгоо',
  'Runtime contract': 'Ажиллах үеийн гэрээ',
  'Runtime contract aligned': 'Ажиллах үеийн гэрээ таарсан',
  'Runtime contract needs attention': 'Ажиллах үеийн гэрээнд анхаарал хэрэгтэй',
  'Inputs': 'Оролтууд',
  'Outputs': 'Гаралтууд',
  'Runtime contract not declared': 'Ажиллах үеийн гэрээ зарлаагүй',
  'Approval binding': 'Баталгааны холболт',
  'Skill dependencies': 'Skill хамаарал',
  'Install preview': 'Суулгалтын урьдчилсан харагдац',
  'Will install': 'Суулгана',
  'Will upgrade': 'Шинэчилнэ',
  'Permission changes': 'Зөвшөөрлийн өөрчлөлт',
  'High risk': 'Өндөр эрсдэл',
  'Will skip': 'Алгасна',
  'Already installed': 'Аль хэдийн суулгасан',
  'Not selected': 'Сонгоогүй',
  'Select all': 'Бүгдийг сонгох',
  'Select none': 'Сонголтыг арилгах',
  'Installable': 'Суулгах боломжтой',
  'Upgradeable': 'Шинэчлэх боломжтой',
  'Upgraded': 'Шинэчилсэн',
  'Skipped': 'Алгассан',
  'Install details': 'Суулгалтын дэлгэрэнгүй',
  'Source package {source} · installed {installed}': 'Эх багц {source} · суулгасан {installed}',
  '{count} dependencies': '{count} хамаарал',
  'Recent installs': 'Сүүлийн суулгалт',
  'Local app package installs and Skill dependency audit': 'Локал аппын багцын суулгалт ба Skill хамаарлын аудит',
  'DataSrv bindings registered': 'DataSrv холболт бүртгэгдсэн',
  'DataSrv bindings partially registered': 'DataSrv холболт хэсэгчлэн бүртгэгдсэн',
  'DataSrv bindings not registered': 'DataSrv холболт бүртгэгдээгүй',
  'DataSrv binding registration failed': 'DataSrv холболтын бүртгэл амжилтгүй',
  'Audit DataSrv': 'DataSrv аудит хийх',
  'Auditing': 'Аудит хийж байна',
  'DataSrv app installations': 'DataSrv апп суулгалт',
  'DataSrv app installation not found': 'DataSrv апп суулгалт олдсонгүй',
  'Reading install records': 'Суулгалтын бичлэгийг уншиж байна',
  'Failed to read install records': 'Суулгалтын бичлэгийг уншиж чадсангүй',
  'No app install records yet': 'Аппын суулгалтын бичлэг одоогоор алга',
  'Refresh records': 'Бичлэгийг шинэчлэх',
  'Check dependencies': 'Хамаарлыг шалгах',
  'Checking': 'Шалгаж байна',
  'Repair dependencies': 'Хамаарлыг засах',
  'Repairing': 'Засаж байна',
  'Install record has no app package snapshot': 'Суулгалтын бичлэгт аппын багцын агшин алга',
  'Package SHA': 'Багцын SHA',
  'Installed at': 'Суулгасан цаг',
  'Blocking deps': 'Саатуулах хамаарал',
  'Enterprise app install evidence was not saved. Retry or check DataSrv registration.': 'Байгууллагын аппын суулгалтын нотолгоо хадгалагдаагүй. Дахин оролдох эсвэл DataSrv бүртгэлийг шалгана уу.',
  'Invalid app package': 'Аппын багц хүчингүй',
  'Show details': 'Дэлгэрэнгүй харуулах',
  'Install failed: app did not pass pre-install checks. Run evidence or dependencies may be missing.': 'Суулгалт амжилтгүй: апп суулгалтын өмнөх шалгалтыг даваагүй. Гүйлтийн нотолгоо эсвэл хамаарал дутуу байж болно.',
  'Install failed: required Skill dependencies are unavailable or download failed. Check network or contact admin.': 'Суулгалт амжилтгүй: шаардлагатай Skill хамаарал боломжгүй эсвэл таталт амжилтгүй. Сүлжээг шалгах эсвэл админтай холбоо барина уу.',
  'Install failed: unable to install the app. Please retry or contact admin.': 'Суулгалт амжилтгүй: аппыг суулгаж чадсангүй. Дахин оролдох эсвэл админтай холбоо барина уу.',
  'Install failed: app workflow contract is not aligned. Contact the app publisher to fix.': 'Суулгалт амжилтгүй: аппын ажлын урсгалын гэрээ таараагүй. Засуулахын тулд аппыг нийтлэгчтэй холбоо барина уу.',
  'JSON parse failed': 'JSON задлалт амжилтгүй',
  'Unrecognized app package format': 'Аппын багцын формат танигдсангүй',
  'Close': 'Хаах',
  'About Info': 'Тухай мэдээлэл',
  'Displayed to users at runtime after publishing': 'Нийтлэсний дараа ажиллах үед хэрэглэгчид харагдана',
  'Author': 'Зохиогч',
  'e.g. John Smith': 'жишээ нь Бат',
  'Copyright': 'Зохиогчийн эрхийн мэдэгдэл',
  'e.g. Copyright © 2026 MyCompany': 'жишээ нь Copyright © 2026 MyCompany',
  'Website': 'Албан ёсны сайт',
  'e.g. https://example.com': 'жишээ нь https://example.com',
  'Contact Email': 'Холбоо барих имэйл',
  'e.g. support@example.com': 'жишээ нь support@example.com',
  'About': 'Тухай',
  'No about information provided by the developer': 'Хөгжүүлэгч тухай мэдээлэл өгөөгүй',
  'Approval app': 'Баталгааны апп',
  'Business app': 'Бизнесийн апп',
  'Tool app': 'Хэрэгслийн апп',
  'Automation': 'Автоматжуулалт',
  'Built-in': 'Суурилагдсан',
  'Market': 'Зах зээл',
  'Local': 'Локал',
  'Expense': 'Зардал',
  'Payment': 'Төлбөр',
  'Invoice': 'Нэхэмжлэх',
  'Warehouse': 'Агуулах',
  'Inventory': 'Нөөц',
  'Customer': 'Харилцагч',
  'People': 'Хүмүүс',
  'Contract': 'Гэрээ',
  'Review': 'Хяналт',
  'Sheet': 'Хүснэгт',
  'Report': 'Тайлан',
  'Dashboard': 'Хянах самбар',
  'Database': 'Өгөгдлийн сан',
  'Clean': 'Цэвэрлэгээ',
  'Logistics': 'Логистик',
  'Calendar': 'Хуанли',
  'Web': 'Вэб',
  'Sync': 'Синк',
  'Agent': 'Agent',
  'Store': 'Дэлгүүр',
  'Blue': 'Цэнхэр',
  'Green': 'Ногоон',
  'Plum': 'Чавга',
  'Amber': 'Хув',
  'Teal': 'Номин',
  'Slate': 'Хөх саарал',
  'Brown': 'Хүрэн',
  'Indigo': 'Индиго',
  'Gray': 'Саарал',
  'Document workspace': 'Баримтын ажлын талбар',
  'Classic split': 'Сонгодог хуваалт',
  'Left navigation': 'Зүүн навигаци',
  'Comfortable': 'Тав тухтай',
  'Compact': 'Нягт',
  'Spacious': 'Зайтай',
  'Left': 'Зүүн',
  'Center': 'Төв',
  'Right': 'Баруун',
  'Modal': 'Модал',
  'Request form': 'Хүсэлтийн маягт',
  'Result feedback': 'Үр дүнгийн санал',
  'Operation form': 'Үйлдлийн маягт',
  'Record list': 'Бичлэгийн жагсаалт',
  'Record detail': 'Бичлэгийн дэлгэрэнгүй',
  'Output panel': 'Гаралтын самбар',
  'File queue': 'Файлын дараалал',
  'Parameters': 'Параметр',
  'Preview': 'Урьдчилан харах',
  'Tool panel': 'Хэрэгслийн самбар',
  'Instances': 'Тохиолдол',
  'Detail': 'Дэлгэрэнгүй',
  'List': 'Жагсаалт',
  'MiniAPP panel': 'MiniAPP самбар',
  'MaClaw MiniAPP Studio': 'MaClaw MiniAPP студи',
  'MaClaw MiniAPP Skill': 'MaClaw MiniAPP Skill',
  'MiniAPP Skill': 'MiniAPP Skill',
  'MiniAPP Studio manual': 'MiniAPP студийн гарын авлага',
  'Open the MaClaw MiniAPP Studio manual': 'MaClaw MiniAPP студийн гарын авлагыг нээх',
  'Open MiniAPP Panel': 'MiniAPP самбарыг нээх',
  'MiniAPP entry created in MaClaw MiniAPP Studio.': 'MaClaw MiniAPP студид үүссэн MiniAPP орох цэг.',
  'Skill installed, but no installable MaClaw MiniAPP definition was found (missing maclaw.app.json).': 'Skill суусан боловч суулгах боломжтой MaClaw MiniAPP тодорхойлолт олдсонгүй (maclaw.app.json дутуу).',
  'Save to Skill and run this version successfully in the MiniAPP panel before uploading to SkillMarket.': 'SkillMarket руу байршуулахаас өмнө Skill-д хадгалаад энэ хувилбарыг MiniAPP самбарт амжилттай ажиллуулна уу.',
  'Found from installed capabilities and synced to the MiniAPP panel': 'Суулгасан чадавхаас олдож, MiniAPP самбарт синк хийгдсэн',
  'Hub URL is missing, so the MaClaw MiniAPP Studio manual cannot be opened.': 'Hub URL алга тул MaClaw MiniAPP студийн гарын авлагыг нээх боломжгүй.',
  'Failed to open the MaClaw MiniAPP Studio manual': 'MaClaw MiniAPP студийн гарын авлагыг нээж чадсангүй',
  'No MaClaw MiniAPP skills yet': 'MaClaw MiniAPP Skill одоогоор алга',
  'MaClaw MiniAPP skills contain maclaw.app.json or maclaw.apps.json and can be opened from the MiniAPP panel after registration.': 'MaClaw MiniAPP Skill-д maclaw.app.json эсвэл maclaw.apps.json багтдаг бөгөөд бүртгэсний дараа MiniAPP самбараас нээж болно.',
  'No MaClaw MiniAPP skills. Install from the Capability Market.': 'MaClaw MiniAPP Skill алга. Чадавхын зах зээлээс суулгана уу.',
  'Browse the Market to find MaClaw MiniAPP skills →': 'MaClaw MiniAPP Skill олохын тулд зах зээлийг үзнэ үү →',
  'MiniAPP definition': 'MiniAPP тодорхойлолт',
  'Edit MiniAPP Skill': 'MiniAPP Skill засах',
  'With source "local" and no install ref, receivers can only install this dependency from the embedded bundle.': 'Эх сурвалж нь «local» бөгөөд суулгалтын лавлагаа байхгүй бол хүлээн авагч энэ хамаарлыг зөвхөн багтаасан багцаас суулгана.',
  'Dependency warnings': 'Хамаарлын анхааруулга',
  'Density': 'Нягтрал',
  'Primary region': 'Үндсэн хэсэг',
  'Output region': 'Гаралтын хэсэг',
  'Region placement': 'Хэсгийн байрлал',
  'Region order': 'Хэсгийн дараалал',
  'Artifact': 'Бүтээгдэхүүн',
  'Artifacts': 'Бүтээгдэхүүнүүд',
  'Document': 'Баримт',
  'File': 'Файл',
  'Notification': 'Мэдэгдэл',
  'Workflow result': 'Ажлын урсгалын үр дүн',
  'Approval instance': 'Баталгааны тохиолдол',
};

export function appsMn(lang: string | undefined | null, en: string): string {
  if (normalizeLang(lang) !== 'mn') return en;
  return appsMnByEnglish[en] || en;
}

const publishQueueStatusEnglish: Record<string, string> = {
  pending_review: 'Waiting for enterprise market review',
  review_failed: 'Changes requested',
  approved: 'Approved',
  published: 'Published',
  deprecated: 'Deprecated',
  revoked: 'Revoked',
};

/** Publish-queue protocol tokens. Mongolian uses the apps glossary; other languages keep the stored token. */
export function appsPublishQueueChannelLabel(lang: string | undefined | null, channel: string): string {
  const value = channel.trim();
  if (normalizeLang(lang) !== 'mn') return value;
  if (value === 'local') return appsMn(lang, 'Local');
  if (value === 'hub') return 'Hub';
  return value;
}

/** Current publish-queue row. Mongolian uses the same sentences as the publish card; other languages keep the stored token. */
export function appsPublishQueueStatusLabel(lang: string | undefined | null, status: string, channel = ''): string {
  const value = status.trim();
  if (normalizeLang(lang) !== 'mn') return value;
  if (value === 'submitted' && channel.trim() === 'local') return appsMn(lang, 'Local sync pending');
  if (value === 'submitted') return appsMn(lang, 'Waiting for enterprise market review');
  const en = publishQueueStatusEnglish[value];
  return en ? appsMn(lang, en) : value;
}

/** Audit event status. This is the stored token, not the card's derived sentence. */
export function appsPublishQueueEventStatusLabel(lang: string | undefined | null, status: string): string {
  const value = status.trim();
  if (normalizeLang(lang) !== 'mn') return value;
  if (value === 'submitted') return appsMn(lang, 'Submitted');
  const en = publishQueueStatusEnglish[value];
  return en ? appsMn(lang, en) : value;
}

const publishQueueMessageExact: Record<string, string> = {
  'queued locally for enterprise market sync': 'Байгууллагын зах зээлийн синкт зориулж локал дараалалд оруулсан',
  'enterprise Hub review state refreshed': 'Enterprise Hub хяналтын төлөв шинэчлэгдлээ',
  'enterprise Hub approved this MaClaw App': 'Enterprise Hub энэ MaClaw аппыг зөвшөөрлөө',
  'enterprise Hub rejected this MaClaw App': 'Enterprise Hub энэ MaClaw аппыг татгалзлаа',
  'ready for one-click': 'Нэг товчоор нийтлэхэд бэлэн',
  'blocked: fix package readiness before publish': 'Хаалттай: нийтлэхийн өмнө багцын бэлэн байдлыг засна уу',
  'local queue OK; remote targets will partially fail': 'Локал дараалал бэлэн; алсын зорилт хэсэгчлэн амжилтгүй байж болно',
  'local queue OK; SkillMarket may fail': 'Локал дараалал бэлэн; SkillMarket амжилтгүй байж болно',
  'local queue OK; enterprise Hub pack may fail': 'Локал дараалал бэлэн; Enterprise Hub багц амжилтгүй байж болно',
  'ready for one-click (skill market can be skipped)': 'Нэг товчоор нийтлэхэд бэлэн (SkillMarket алгасаж болно)',
  'queued locally': 'Локал дараалалд оруулсан',
  'local queue ready': 'Локал дараалал бэлэн',
  'enterprise hub pack already synced': 'Байгууллагын Hub багц аль хэдийн синк хийгдсэн',
  'enterprise hub pack submitted (after dep stamp retry)': 'Байгууллагын Hub багц илгээгдсэн (хамаарлын тэмдэглэгээг давтан оролдсоны дараа)',
  'enterprise hub pack submitted': 'Байгууллагын Hub багц илгээгдсэн',
  'enterprise Hub marketplace URL/token not configured': 'Байгууллагын Hub зах зээлийн URL/Token тохируулаагүй',
  'remote_email / Hub enrollment incomplete': 'remote_email / Hub бүртгэл дутуу',
  'package payload is empty': 'Багцын агуулга хоосон',
  'local queue OK': 'Локал дараалал бэлэн',
  'remote targets will partially fail': 'алсын зорилт хэсэгчлэн амжилтгүй байж болно',
  'SkillMarket may fail': 'SkillMarket амжилтгүй байж болно',
  'enterprise Hub pack may fail': 'Enterprise Hub багц амжилтгүй байж болно',
  'deps need bundle or skill upload first': 'Эхлээд хамаарлыг багцлах эсвэл Skill байршуулах хэрэгтэй',
  're-login SkillMarket / Hub session': 'SkillMarket / Hub сессэд дахин нэвтэрнэ үү',
  'configure enterprise Hub marketplace URL + token': 'Enterprise Hub зах зээлийн URL болон Token-ыг тохируулна уу',
  'set remote_email / complete Hub enrollment': 'remote_email-г бөглөж, Hub бүртгэлийг дуусгана уу',
  'HubCenter unreachable — check network': 'HubCenter хүрэхгүй. Сүлжээг шалгана уу',
  'package fingerprint mismatch — re-queue or retry': 'Багцын хурууны хээ зөрүүтэй. Дараалалд дахин оруулах эсвэл дахин оролдоно уу',
  'partial failed': 'хэсэгчлэн амжилтгүй',
  'enterprise Hub TLS certificate is expired or not yet valid; renew the certificate or correct the Hub server clock before retrying': 'Enterprise Hub-ийн TLS гэрчилгээний хугацаа дууссан эсвэл хараахан хүчинтэй биш. Гэрчилгээг сунгана уу, эсвэл Hub серверийн цагийг засна уу, дараа нь дахин оролдоно уу.',
  'required Skill dependencies need to be bundled or published before retrying': 'Шаардлагатай Skill хамаарлыг дахин оролдохоос өмнө багцлах эсвэл нийтлэх хэрэгтэй',
  'publish the dependency Skill, refresh dependency verification, then retry': 'Хамаарлын Skill-ийг нийтэлж, хамаарлын шалгалтыг шинэчлээд дахин оролдоно уу',
  'a required Skill is missing its install reference; publish the dependency Skill, refresh dependency verification, then retry': 'Шаардлагатай Skill-ийн суулгалтын лавлагаа алга. Хамаарлын Skill-ийг нийтэлж, хамаарлын шалгалтыг шинэчлээд дахин оролдоно уу.',
};

const publishQueueSkipReason: Record<string, string> = {
  already_published: 'аль хэдийн нийтэлсэн',
  primary_skill_already_published: 'үндсэн Skill аль хэдийн нийтэлсэн',
};

function translatePublishQueueSkipReason(reason: string): string {
  return publishQueueSkipReason[reason] || reason;
}

function translatePublishQueueErrorTail(tail: string): string {
  const exact = publishQueueMessageExact[tail];
  if (exact) return exact;
  let match = /^SkillMarket session expired — re-login Hub\/SkillMarket \(([\s\S]*)\)$/.exec(tail);
  if (match) return `SkillMarket сессийн хугацаа дууссан. Hub/SkillMarket руу дахин нэвтэрнэ үү (${match[1]})`;
  match = /^network\/unreachable HubCenter \(([\s\S]*)\)$/.exec(tail);
  if (match) return `Сүлжээ эсвэл HubCenter хүрэхгүй (${match[1]})`;
  match = /^package fingerprint mismatch \(([\s\S]*)\)$/.exec(tail);
  if (match) return `Багцын хурууны хээ зөрүүтэй (${match[1]})`;
  match = /^a required Skill is missing its install reference; ([\s\S]+)$/.exec(tail);
  if (match) return `Шаардлагатай Skill-ийн суулгалтын лавлагаа алга. ${translatePublishQueueErrorTail(match[1])}`;
  return tail;
}

function translatePublishQueueHints(tail: string): string {
  return tail.split(' · ').map((piece) => {
    const trimmed = piece.trim();
    return publishQueueMessageExact[trimmed] || trimmed;
  }).join(' · ');
}

function translatePublishQueueClause(part: string): string {
  const trimmed = part.trim();
  if (!trimmed) return '';
  const exact = publishQueueMessageExact[trimmed];
  if (exact) return exact;
  const cut = trimmed.indexOf(' · ');
  if (cut >= 0) {
    const head = translatePublishQueueClause(trimmed.slice(0, cut));
    const tail = translatePublishQueueHints(trimmed.slice(cut + 3));
    return tail ? `${head} · ${tail}` : head;
  }
  let match = /^skill market upload ok \(([^)]*)\), stamped (.+) dep ref$/.exec(trimmed);
  if (match) return `SkillMarket байршуулалт амжилттай (${match[1]}), ${match[2]} хамаарлын лавлагаа тэмдэглэсэн`;
  match = /^skill market upload ok \(([^)]*)\)$/.exec(trimmed);
  if (match) return `SkillMarket байршуулалт амжилттай (${match[1]})`;
  match = /^skill market skipped \(([^)]*)\), stamped (.+) dep ref$/.exec(trimmed);
  if (match) return `SkillMarket алгассан (${translatePublishQueueSkipReason(match[1])}), ${match[2]} хамаарлын лавлагаа тэмдэглэсэн`;
  match = /^skill market skipped \(([^)]*)\)$/.exec(trimmed);
  if (match) return `SkillMarket алгассан (${translatePublishQueueSkipReason(match[1])})`;
  match = /^enterprise hub pack failed: ([\s\S]+)$/.exec(trimmed);
  if (match) return `Байгууллагын Hub багц амжилтгүй: ${translatePublishQueueErrorTail(match[1])}`;
  match = /^skill market failed: ([\s\S]+)$/.exec(trimmed);
  if (match) return `SkillMarket амжилтгүй: ${translatePublishQueueErrorTail(match[1])}`;
  return trimmed;
}

function translatePublishQueueBody(text: string): string {
  const exact = publishQueueMessageExact[text];
  if (exact) return exact;
  // A " · " hint is peeled inside each clause. Splitting first keeps a later
  // "; partial failed" marker from sticking to the hint.
  return text.split('; ').map((part) => translatePublishQueueClause(part)).filter(Boolean).join('; ');
}

/**
 * Go joins one-click queue clauses with "; ". Mongolian translates that grammar.
 * English and Chinese keep the stored sentence. Unknown clauses stay as sent.
 */
export function appsPublishQueueMessage(lang: string | undefined | null, message: string): string {
  const raw = message.trim();
  if (!raw || normalizeLang(lang) !== 'mn') return raw;
  const exact = publishQueueMessageExact[raw];
  if (exact) return exact;
  let text = raw;
  const embedded = [
    'enterprise Hub TLS certificate is expired or not yet valid; renew the certificate or correct the Hub server clock before retrying',
    'a required Skill is missing its install reference; publish the dependency Skill, refresh dependency verification, then retry',
  ];
  for (const sentence of embedded) {
    text = text.split(sentence).join(publishQueueMessageExact[sentence]);
  }
  text = text.split('a required Skill is missing its install reference; ').join('Шаардлагатай Skill-ийн суулгалтын лавлагаа алга. ');
  return translatePublishQueueBody(text);
}

const preflightFriendly = {
  missing_run_evidence: {
    en: 'A successful run is missing. Use Fix, run the app once, then return here to publish.',
    zhHans: '缺少一次成功运行证据。请点击“去修复”，完整运行应用一次后返回此处发布。',
    zhHant: '缺少一次成功執行證據。請點擊「去修復」，完整執行應用一次後返回此處發佈。',
    mn: 'Амжилттай гүйлтийн нотолгоо алга. «Засвар» товчоор аппыг нэг удаа бүтэн ажиллуулаад, нийтлэхийн тулд энд буцаж ирнэ үү.',
  },
  rerun_evidence: {
    en: 'Run evidence does not match the current app definition. Use Fix, run this version again, then return here to publish.',
    zhHans: '运行证据与当前应用定义不一致。请点击“去修复”，重新运行当前版本后返回此处发布。',
    zhHant: '執行證據與目前應用定義不一致。請點擊「去修復」，重新執行目前版本後返回此處發佈。',
    mn: 'Гүйлтийн нотолгоо одоогийн аппын тодорхойлолттой таарахгүй. «Засвар» товчоор энэ хувилбарыг дахин ажиллуулаад, нийтлэхийн тулд энд буцаж ирнэ үү.',
  },
  unpublished_dependency: {
    en: 'A required Skill is not published or bundled, so the Enterprise Hub publish may fail. Resolve the dependency first.',
    zhHans: '有必需 Skill 尚未发布或打包，Enterprise Hub 发布可能失败。请先处理依赖。',
    zhHant: '有必需 Skill 尚未發佈或打包，Enterprise Hub 發佈可能失敗。請先處理依賴。',
    mn: 'Шаардлагатай Skill нийтлэгдээгүй эсвэл багцад ороогүй тул Enterprise Hub нийтлэл амжилтгүй байж болно. Эхлээд хамаарлыг шийдвэрлэнэ үү.',
  },
  hub_tls: {
    en: 'The Enterprise Hub TLS certificate is unusable. Renew the certificate or correct the server clock, then retry.',
    zhHans: 'Enterprise Hub 的 TLS 证书不可用。请更新证书或校正服务器时间后重试。',
    zhHant: 'Enterprise Hub 的 TLS 憑證無法使用。請更新憑證或校正伺服器時間後重試。',
    mn: 'Enterprise Hub-ийн TLS гэрчилгээ ашиглах боломжгүй. Гэрчилгээг сунгана уу, эсвэл серверийн цагийг засна уу, дараа нь дахин оролдоно уу.',
  },
} as const;

export type AppsPreflightCheckKind = keyof typeof preflightFriendly;

/** Friendly preflight rewrite. Chinese stays Chinese, Mongolian is Mongolian, other languages use English. */
export function appsPreflightCheckText(lang: string | undefined | null, kind: AppsPreflightCheckKind): string {
  const row = preflightFriendly[kind];
  if (normalizeLang(lang) === 'mn') return row.mn;
  return localizeText(lang, row.en, row.zhHans, row.zhHant);
}

const oneClickErrorCopy = {
  location: {
    en: 'Location',
    zhHans: '位置',
    zhHant: '位置',
    mn: 'Байрлал',
  },
  next_step: {
    en: 'Next step',
    zhHans: '下一步',
    zhHant: '下一步',
    mn: 'Дараагийн алхам',
  },
  technical_details: {
    en: 'Technical details',
    zhHans: '技术详情',
    zhHant: '技術詳情',
    mn: 'Техникийн дэлгэрэнгүй',
  },
  dep_not_ready: {
    en: 'Required Skill dependency is not ready for Enterprise Hub submission.',
    zhHans: '必需 Skill 依赖尚未准备好，无法提交到 Enterprise Hub。',
    zhHant: '必需 Skill 依賴尚未準備好，無法提交到 Enterprise Hub。',
    mn: 'Шаардлагатай Skill хамаарал Enterprise Hub илгээлтэд бэлэн биш.',
  },
  hub_tls_rejected: {
    en: 'Enterprise Hub rejected the connection because its TLS certificate is expired or not yet valid.',
    zhHans: 'Enterprise Hub 拒绝了连接，因为 TLS 证书已过期或尚未生效。',
    zhHant: 'Enterprise Hub 拒絕了連線，因為 TLS 憑證已過期或尚未生效。',
    mn: 'Enterprise Hub холболтыг татгалзлаа. TLS гэрчилгээний хугацаа дууссан эсвэл хараахан хүчинтэй биш.',
  },
  remote_publish_failed: {
    en: 'Remote publish failed.',
    zhHans: '远程发布失败。',
    zhHant: '遠端發佈失敗。',
    mn: 'Алсын нийтлэл амжилтгүй.',
  },
  dep_retry: {
    en: 'Publish the dependency Skill, refresh dependency verification, then retry.',
    zhHans: '请先发布依赖 Skill，刷新依赖校验后再试。',
    zhHant: '請先發佈依賴 Skill，重新整理依賴校驗後再試。',
    mn: 'Хамаарлын Skill-ийг нийтэлж, хамаарлын шалгалтыг шинэчлээд дахин оролдоно уу',
  },
  hub_tls_retry: {
    en: 'Renew the Enterprise Hub TLS certificate or correct the Hub server clock, then retry.',
    zhHans: '请更新 Enterprise Hub 的 TLS 证书或校正 Hub 服务器时间后重试。',
    zhHant: '請更新 Enterprise Hub 的 TLS 憑證或校正 Hub 伺服器時間後重試。',
    mn: 'Enterprise Hub-ийн TLS гэрчилгээг сунгана уу, эсвэл Hub серверийн цагийг засна уу, дараа нь дахин оролдоно уу.',
  },
  review_and_retry: {
    en: 'Review the technical details and retry after resolving the issue.',
    zhHans: '请查看技术详情，解决问题后重试。',
    zhHant: '請查看技術詳情，解決問題後重試。',
    mn: 'Техникийн дэлгэрэнгүйг нягталж, асуудлыг зассаны дараа дахин оролдоно уу.',
  },
} as const;

export type AppsOneClickErrorKind = keyof typeof oneClickErrorCopy;

/** One-click error panel chrome and frontend fallbacks. Server issue text is not rewritten here. */
export function appsOneClickErrorText(lang: string | undefined | null, kind: AppsOneClickErrorKind): string {
  const row = oneClickErrorCopy[kind];
  if (normalizeLang(lang) === 'mn') return row.mn;
  return localizeText(lang, row.en, row.zhHans, row.zhHant);
}

const reviewIssueExact: Record<string, string> = {
  'package not ready': 'Багц бэлэн биш',
  'Remote publish rejected the package': 'Алсын нийтлэл багцыг татгалзлаа',
  'governance metadata is missing': 'Засаглалын мета өгөгдөл алга',
  'save the app from App Studio so workspace layout, result contract, dependencies and test evidence are recorded': 'Ажлын талбарын зохион байгуулалт, үр дүнгийн гэрээ, хамаарал, тестийн нотолгоо бичигдэхийн тулд аппыг App Studio-оос хадгална уу',
  'workspace layout evidence is required': 'Ажлын талбарын зохион байгуулалтын нотолгоо шаардлагатай',
  'generate and save the visual MaClaw App workspace layout before submitting to Hub': 'Hub руу илгээхийн өмнө MaClaw App-ын ажлын талбарын харагдах зохион байгуулалтыг үүсгээд хадгална уу',
  'workspace layout must include visual regions': 'Ажлын талбарын зохион байгуулалтад харагдах муж байх ёстой',
  'save the App Studio layout after arranging input, workbench and output regions': 'Оролт, ажлын хэсэг, гаралтын мужийг байрлуулсны дараа App Studio-ын зохион байгуулалтыг хадгална уу',
  'workspace layout must include an input region': 'Ажлын талбарын зохион байгуулалтад оролтын муж байх ёстой',
  'add or mark one layout region as the data entry/input area': 'Нэг мужийг өгөгдөл оруулах хэсэг болгон нэмж эсвэл тэмдэглэнэ үү',
  'workspace layout must include an output region': 'Ажлын талбарын зохион байгуулалтад гаралтын муж байх ёстой',
  'add or mark one layout region as the result/output area': 'Нэг мужийг үр дүнгийн гаралт болгон нэмж эсвэл тэмдэглэнэ үү',
  'workspace layout fingerprint does not match the saved manifest UI layout': 'Ажлын талбарын хурууны хээ хадгалсан манифестын UI зохион байгуулалттай таарахгүй',
  'save the App Studio layout again so app.ui, binding.ui, and governance.workspaceLayout share the same fingerprint': 'app.ui, binding.ui, governance.workspaceLayout нэг хурууны хээтэй болохын тулд App Studio-ын зохион байгуулалтыг дахин хадгална уу',
  'workspace layout fingerprint does not match the saved layout regions': 'Ажлын талбарын хурууны хээ хадгалсан мужуудтай таарахгүй',
  'save the App Studio layout again after moving, hiding, or reordering workspace regions': 'Мужийг зөөсөн, нуусан эсвэл дарааллыг өөрчилсний дараа App Studio-ын зохион байгуулалтыг дахин хадгална уу',
  'result contract is required': 'Үр дүнгийн гэрээ шаардлагатай',
  'declare whether the app returns approval status, content, business data, documents or artifacts': 'Апп баталгааны төлөв, агуулга, бизнесийн өгөгдөл, баримт бичиг эсвэл бүтээгдэхүүн буцаадаг эсэхийг зарлана уу',
  'result contract must declare a primary result': 'Үр дүнгийн гэрээнд үндсэн үр дүнг зарлах ёстой',
  'set resultContract.primary so installers and DataSrv can classify the app output': 'Суулгагч болон DataSrv гаралтыг ангилахын тулд resultContract.primary-г тохируулна уу',
  'test evidence is required': 'Тестийн нотолгоо шаардлагатай',
  'run the App Studio test protocol and submit the package produced after the test passes': 'App Studio-ын тестийн протоколыг ажиллуулаад, тест амжилттай болсны дараах багцыг илгээнэ үү',
  'test evidence must include a run id': 'Тестийн нотолгоонд гүйлтийн ID байх ёстой',
  'rerun the app test protocol and keep the generated run evidence': 'Аппын тестийн протоколыг дахин ажиллуулаад үүссэн гүйлтийн нотолгоог хадгална уу',
  'test evidence must include the test protocol fingerprint': 'Тестийн нотолгоонд тестийн протоколын хурууны хээ байх ёстой',
  'save the test protocol and rerun the app test so evidence is tied to the tested protocol': 'Тестийн протоколыг хадгалаад аппын тестийг дахин ажиллуулж, нотолгоог шалгасан протоколтой холбоно уу',
  'test evidence does not cover the declared result contract': 'Тестийн нотолгоо зарласан үр дүнгийн гэрээг хамрахгүй',
  'rerun the app test and cover every required output type before submitting': 'Илгээхийн өмнө аппын тестийг дахин ажиллуулж, шаардлагатай гаралтын төрөл бүрийг хамруулна уу',
  'approval app test evidence must include approval instance evidence': 'Баталгааны аппын тестийн нотолгоонд баталгааны тохиолдлын нотолгоо байх ёстой',
  'run the approval app and verify my applications, my approvals or handled views before submitting': 'Илгээхийн өмнө баталгааны аппыг ажиллуулж, миний өргөдөл, миний баталгаа эсвэл шийдвэрлэсэн харалтыг шалгана уу',
  'approval instance evidence must include an instance id or approval id': 'Баталгааны тохиолдлын нотолгоонд тохиолдлын ID эсвэл баталгааны ID байх ёстой',
  'sync the tested approval instance to DataSrv and keep the returned approval id': 'Шалгасан баталгааны тохиолдлыг DataSrv руу синк хийж, буцаасан баталгааны ID-г хадгална уу',
  'approval instance evidence must include the current or final status': 'Баталгааны тохиолдлын нотолгоонд одоогийн эсвэл эцсийн төлөв байх ёстой',
  'open the approval instance view after running the workflow and save the refreshed evidence': 'Ажлын урсгалыг ажиллуулсны дараа баталгааны тохиолдлын харалтыг нээж, шинэчлэгдсэн нотолгоог хадгална уу',
  'approval app test evidence must verify an approval instance view': 'Баталгааны аппын тестийн нотолгоо баталгааны тохиолдлын харалтыг шалгасан байх ёстой',
  'verify my applications, my approvals, handled or attention view for the tested approval instance': 'Шалгасан баталгааны тохиолдлын миний өргөдөл, миний баталгаа, шийдвэрлэсэн эсвэл анхаарах харалтыг шалгана уу',
  'dependency verification is required': 'Хамаарлын шалгалт шаардлагатай',
  'run dependency planning so every required Skill dependency has a resolvable install reference': 'Шаардлагатай Skill хамаарал бүрт олох боломжтой суулгалтын лавлагаа гарахын тулд хамаарлын төлөвлөгөөг ажиллуулна уу',
  'dependency verification failed': 'Хамаарлын шалгалт амжилтгүй',
  'install, publish or fix the missing required Skill dependencies before submitting': 'Илгээхийн өмнө дутуу шаардлагатай Skill хамаарлыг суулгаж, нийтэлж эсвэл засна уу',
  'dependency verification contains blocked dependencies': 'Хамаарлын шалгалтад хаагдсан хамаарал байна',
  'resolve blocked Skill dependencies before submitting the app package': 'Аппын багцыг илгээхийн өмнө хаагдсан Skill хамаарлыг шийдвэрлэнэ үү',
  'fix blocked dependencies and regenerate dependency verification': 'Хаагдсан хамаарлыг засаад хамаарлын шалгалтыг дахин үүсгэнэ үү',
  'dependency verification contains missing dependencies': 'Хамаарлын шалгалтад дутуу хамаарал байна',
  'install or publish every required dependency and regenerate dependency verification': 'Шаардлагатай хамаарал бүрийг суулгаж эсвэл нийтэлж, хамаарлын шалгалтыг дахин үүсгэнэ үү',
  'dependency verification must include checked Skill dependencies': 'Хамаарлын шалгалтад шалгасан Skill хамаарал байх ёстой',
  'submit the package after PlanMaclawAppInstall records the checked dependency list': 'PlanMaclawAppInstall шалгасан хамаарлын жагсаалтыг бичсэний дараа багцыг илгээнэ үү',
  'verified required dependency must include install_ref': 'Шалгасан шаардлагатай хамааралд install_ref байх ёстой',
  'publish the dependency Skill to Hub or SkillMarket and regenerate dependency verification': 'Хамаарлын Skill-ийг Hub эсвэл SkillMarket дээр нийтэлж, хамаарлын шалгалтыг дахин үүсгэнэ үү',
  'approval app requires workflow mapping': 'Баталгааны аппад ажлын урсгалын харгалзуулалт шаардлагатай',
  'map submit, approval, result and attention nodes before submitting the approval app': 'Баталгааны аппыг илгээхийн өмнө илгээх, баталгаа, үр дүн, анхаарах зангилааг харгалзуулна уу',
  'approval app workflow mapping must include submit node': 'Баталгааны аппын ажлын урсгалын харгалзуулалтад илгээх зангилаа байх ёстой',
  'approval app workflow mapping must include approval node': 'Баталгааны аппын ажлын урсгалын харгалзуулалтад баталгааны зангилаа байх ёстой',
  'approval app workflow mapping must include result node': 'Баталгааны аппын ажлын урсгалын харгалзуулалтад үр дүнгийн зангилаа байх ёстой',
  'complete the approval workflow node mapping in App Studio': 'App Studio дотор баталгааны ажлын урсгалын зангилааны харгалзуулалтыг гүйцээнэ үү',
  'approval app requires workflow contract': 'Баталгааны аппад ажлын урсгалын гэрээ шаардлагатай',
  'declare the approval workflow skill, object role and result mapping before submitting': 'Илгээхийн өмнө баталгааны ажлын урсгалын Skill, объектын үүрэг, үр дүнгийн харгалзуулалтыг зарлана уу',
  'workflow contract must include workflow skill id': 'Ажлын урсгалын гэрээнд ажлын урсгалын Skill ID байх ёстой',
  'bind the approval workflow Skill used by this app': 'Энэ аппын баталгааны ажлын урсгалын Skill-ийг холбоно уу',
  'workflow contract must include approval object role': 'Ажлын урсгалын гэрээнд баталгааны объектын үүрэг байх ёстой',
  'set the business object role that the approval workflow manages': 'Баталгааны ажлын урсгалын удирдах бизнесийн объектын үүргийг тохируулна уу',
  'declare workflow_result, approval_instance, outputs and artifacts so Hub can verify approval workflow result packages': 'Hub баталгааны ажлын урсгалын үр дүнгийн багцыг шалгахын тулд workflow_result, approval_instance, outputs, artifacts-ыг зарлана уу',
  'missing governance metadata': 'Засаглалын мета өгөгдөл алга',
  'include dependency, workspace layout, and test evidence metadata before publishing': 'Нийтлэхийн өмнө хамаарал, ажлын талбарын зохион байгуулалт, тестийн нотолгооны мета өгөгдлийг оруулна уу',
  'missing successful local run evidence': 'Амжилттай локал гүйлтийн нотолгоо алга',
  'run the app once in App Studio before submitting to the capability market': 'Чадавхын зах зээлд илгээхийн өмнө App Studio дотор аппыг нэг удаа ажиллуулна уу',
  'missing workspace layout evidence': 'Ажлын талбарын зохион байгуулалтын нотолгоо алга',
  'save the generated UI layout in the app manifest before publishing': 'Нийтлэхийн өмнө үүссэн UI зохион байгуулалтыг аппын манифестэд хадгална уу',
  'missing result contract': 'Үр дүнгийн гэрээ алга',
  'declare the app output contract before submitting to the capability market': 'Чадавхын зах зээлд илгээхийн өмнө аппын гаралтын гэрээг зарлана уу',
  'missing workflow node mapping': 'Ажлын урсгалын зангилааны харгалзуулалт алга',
  'save the approval workflow node mapping in App Studio before submitting to the capability market': 'Чадавхын зах зээлд илгээхийн өмнө баталгааны ажлын урсгалын зангилааны харгалзуулалтыг App Studio-д хадгална уу',
  'missing test protocol': 'Тестийн протокол алга',
  'save the App Studio test protocol with sample input, expected output, roles, scopes, and risk before submitting': 'Илгээхийн өмнө жишээ оролт, хүлээгдэж буй гаралт, үүрэг, хамрах хүрээ, эрсдэлийн хамт App Studio-ын тестийн протоколыг хадгална уу',
  'invalid test protocol schema': 'Тестийн протоколын схем буруу',
  'set testProtocol.schema to maclaw.app.test_protocol.v1': 'testProtocol.schema-г maclaw.app.test_protocol.v1 болгоно уу',
  'test protocol is missing sample input': 'Тестийн протоколд жишээ оролт алга',
  'include the App Studio test sample input used by the local run': 'Локал гүйлтэд ашигласан App Studio-ын тестийн жишээ оролтыг оруулна уу',
  'test protocol is missing expected output': 'Тестийн протоколд хүлээгдэж буй гаралт алга',
  'include expected_output or expectedOutput so the local run can be reproduced': 'Локал гүйлтийг дахин гаргахын тулд expected_output эсвэл expectedOutput-ыг оруулна уу',
  'run evidence is not linked to a test protocol fingerprint': 'Гүйлтийн нотолгоо тестийн протоколын хурууны хээтэй холбогдоогүй',
  'store the test protocol fingerprint produced by the App Studio test run': 'App Studio-ын тестийн гүйлтийн гаргасан тестийн протоколын хурууны хээг хадгална уу',
  'run evidence test protocol fingerprint does not match the current test protocol': 'Гүйлтийн нотолгооны тестийн протоколын хурууны хээ одоогийн тестийн протоколтой таарахгүй',
  'rerun the app test after editing the test protocol': 'Тестийн протоколыг зассны дараа аппын тестийг дахин ажиллуулна уу',
  'run evidence is missing the current app definition hash': 'Гүйлтийн нотолгоонд одоогийн аппын тодорхойлолтын хэш алга',
  'run the current app definition again before submitting to the capability market': 'Чадавхын зах зээлд илгээхийн өмнө одоогийн аппын тодорхойлолтыг дахин ажиллуулна уу',
  'run evidence definition hash does not match current app definition': 'Гүйлтийн нотолгооны тодорхойлолтын хэш одоогийн аппын тодорхойлолттой таарахгүй',
  'run evidence is missing the current workspace layout fingerprint': 'Гүйлтийн нотолгоонд одоогийн ажлын талбарын зохион байгуулалтын хурууны хээ алга',
  'rerun the app test after saving the App Studio workspace layout': 'App Studio-ын ажлын талбарын зохион байгуулалтыг хадгалсны дараа аппын тестийг дахин ажиллуулна уу',
  'run evidence workspace layout fingerprint does not match the current workspace layout': 'Гүйлтийн нотолгооны ажлын талбарын хурууны хээ одоогийн зохион байгуулалттай таарахгүй',
  'rerun the app test after editing or saving the workspace layout': 'Ажлын талбарын зохион байгуулалтыг зассан эсвэл хадгалсны дараа аппын тестийг дахин ажиллуулна уу',
  'run the app again and verify every declared required result type is present in the result payload or outputs': 'Аппыг дахин ажиллуулж, зарласан шаардлагатай үр дүнгийн төрөл бүр үр дүнгийн агуулга эсвэл гаралтад байгаа эсэхийг шалгана уу',
  'run the app again and verify the declared primary result is present in the result payload or outputs': 'Аппыг дахин ажиллуулж, зарласан үндсэн үр дүн үр дүнгийн агуулга эсвэл гаралтад байгаа эсэхийг шалгана уу',
  'missing dependency verification': 'Хамаарлын шалгалт алга',
  'run dependency verification before submitting to the capability market': 'Чадавхын зах зээлд илгээхийн өмнө хамаарлын шалгалтыг ажиллуулна уу',
  'invalid dependency verification schema': 'Хамаарлын шалгалтын схем буруу',
  'attach the backend PlanMaclawAppInstall result to governance.dependencyVerification': 'PlanMaclawAppInstall-ын үр дүнг governance.dependencyVerification-д хавсаргана уу',
  'required dependency is missing or blocked': 'Шаардлагатай хамаарал дутуу эсвэл хаагдсан',
  'install or enable required Skill dependencies before submitting': 'Илгээхийн өмнө шаардлагатай Skill хамаарлыг суулгаж эсвэл идэвхжүүлнэ үү',
  'approval workflow contract verification failed': 'Баталгааны ажлын урсгалын гэрээний шалгалт амжилтгүй',
  'refresh dependency verification and align the approval workflow Skill contract before submitting': 'Илгээхийн өмнө хамаарлын шалгалтыг шинэчилж, баталгааны ажлын урсгалын Skill гэрээг тааруулна уу',
  'dependency governance review failed': 'Хамаарлын засаглалын хяналт амжилтгүй',
  'resolve app governance review issues and refresh dependency verification before submitting': 'Илгээхийн өмнө аппын засаглалын хяналтын асуудлыг шийдвэрлэж, хамаарлын шалгалтыг шинэчилнэ үү',
  'refresh dependency verification before submitting': 'Илгээхийн өмнө хамаарлын шалгалтыг шинэчилнэ үү',
  'install or enable the required Skill dependency before submitting': 'Илгээхийн өмнө шаардлагатай Skill хамаарлыг суулгаж эсвэл идэвхжүүлнэ үү',
  'approval app has no approval workflow binding': 'Баталгааны аппад баталгааны ажлын урсгалын холболт алга',
  'declare binding.mis.approvalBindings with event, objectRole, and workflowSkillId': 'binding.mis.approvalBindings дотор event, objectRole, workflowSkillId-г зарлана уу',
  'approval workflow binding is missing workflowSkillId': 'Баталгааны ажлын урсгалын холболтод workflowSkillId алга',
  'bind this approval event to an installed workflow Skill': 'Энэ баталгааны үйл явдлыг суусан ажлын урсгалын Skill-тэй холбоно уу',
  'enable or finish setup for the workflow Skill before running approval instances': 'Баталгааны тохиолдол ажиллуулахаас өмнө ажлын урсгалын Skill-ийн тохиргоог дуусгаж эсвэл идэвхжүүлнэ үү',
  'install the workflow Skill version declared by the app approval binding or workflow contract': 'Аппын баталгааны холболт эсвэл ажлын урсгалын гэрээнд зарласан Skill хувилбарыг суулгана уу',
  'missing approval workflow contract': 'Баталгааны ажлын урсгалын гэрээ алга',
  'declare the workflow skill input, decision output, and status mapping contract before submitting': 'Илгээхийн өмнө ажлын урсгалын Skill-ийн оролт, шийдвэрийн гаралт, төлөвийн харгалзуулалтын гэрээг зарлана уу',
  'invalid approval workflow contract schema': 'Баталгааны ажлын урсгалын гэрээний схем буруу',
  'set workflowContract.schema to maclaw.app.workflow_contract.v1': 'workflowContract.schema-г maclaw.app.workflow_contract.v1 болгоно уу',
  'approval workflow contract does not match approval binding': 'Баталгааны ажлын урсгалын гэрээ баталгааны холболттой таарахгүй',
  'use a workflowSkillId declared by approvalBindings or workflow_skill dependencies': 'approvalBindings эсвэл workflow_skill хамааралд зарласан workflowSkillId-г ашиглана уу',
  'approval workflow contract object role does not match app binding': 'Баталгааны ажлын урсгалын гэрээний объектын үүрэг аппын холболттой таарахгүй',
  'align workflowContract.objectRole with approvalBindings.objectRole or binding.datasrv.objectRole': 'workflowContract.objectRole-ыг approvalBindings.objectRole эсвэл binding.datasrv.objectRole-тай тааруулна уу',
  'approval app test evidence is missing a created approval instance': 'Баталгааны аппын тестийн нотолгоонд үүссэн баталгааны тохиолдол алга',
  'run the approval app in App Studio, create a test approval instance, and save its approval_instance_id in test evidence': 'App Studio дотор баталгааны аппыг ажиллуулж, тестийн баталгааны тохиолдол үүсгээд, approval_instance_id-г тестийн нотолгоонд хадгална уу',
  'approval app test evidence is missing approval instance status': 'Баталгааны аппын тестийн нотолгоонд баталгааны тохиолдлын төлөв алга',
  'save the status observed from the approval instance view after the App Studio test run': 'App Studio-ын тестийн гүйлтийн дараа баталгааны тохиолдлын харалтаас харсан төлөвийг хадгална уу',
  'approval app test evidence is missing the current workflow node': 'Баталгааны аппын тестийн нотолгоонд одоогийн ажлын урсгалын зангилаа алга',
  'save the final current_node observed from the approval workflow instance after the App Studio test run': 'App Studio-ын тестийн гүйлтийн дараа баталгааны ажлын урсгалын тохиолдлоос харсан эцсийн current_node-ыг хадгална уу',
  'approval app test evidence is missing the workflow Skill id': 'Баталгааны аппын тестийн нотолгоонд ажлын урсгалын Skill ID алга',
  'save workflowSkillId/workflow_skill_id in approvalInstance evidence so the market can verify the approval workflow dependency': 'Зах зээл баталгааны ажлын урсгалын хамаарлыг шалгахын тулд approvalInstance нотолгоонд workflowSkillId/workflow_skill_id-г хадгална уу',
  'approval app test evidence is missing business/result status': 'Баталгааны аппын тестийн нотолгоонд бизнес эсвэл үр дүнгийн төлөв алга',
  'save businessStatus/resultStatus from the completed approval workflow instance': 'Дууссан баталгааны ажлын урсгалын тохиолдлоос businessStatus/resultStatus-ыг хадгална уу',
  'approval app test evidence is missing the approval result package': 'Баталгааны аппын тестийн нотолгоонд баталгааны үр дүнгийн багц алга',
  'save resultPayload or outputs on approvalInstance after the approval workflow completes and DataSrv sync has been attempted': 'Баталгааны ажлын урсгал дуусаж, DataSrv синкийг оролдсоны дараа approvalInstance дээр resultPayload эсвэл outputs-ыг хадгална уу',
  'approval app test evidence has not verified the approval instance view': 'Баталгааны аппын тестийн нотолгоо баталгааны тохиолдлын харалтыг шалгаагүй',
  'open the generated approval app instance list and save view verification for my_requests, pending_my_approval, handled, or attention': 'Үүссэн баталгааны аппын тохиолдлын жагсаалтыг нээж, my_requests, pending_my_approval, handled эсвэл attention харалтын шалгалтыг хадгална уу',
  'stored MaClaw App manifest is missing': 'Хадгалсан MaClaw App манифест алга',
  'resubmit the app package before approval': 'Баталгаанаас өмнө аппын багцыг дахин илгээнэ үү',
  'resubmit a valid MaClaw App package before approval': 'Баталгаанаас өмнө хүчинтэй MaClaw App багцыг дахин илгээнэ үү',
  'maclaw app package is not ready for Hub approval: stored manifest is missing': 'MaClaw аппын багц Hub-ын баталгаанд бэлэн биш: хадгалсан манифест алга',
  'maclaw app package is not ready for Hub approval: stored manifest is invalid': 'MaClaw аппын багц Hub-ын баталгаанд бэлэн биш: хадгалсан манифест буруу',
};

const reviewIssuePrefixes: Array<[string, string]> = [
  ['workflow contract must include required output ', 'Ажлын урсгалын гэрээнд шаардлагатай гаралт орсон байх ёстой: '],
  ['run evidence does not cover result contract: ', 'Гүйлтийн нотолгоо үр дүнгийн гэрээг хамарсангүй: '],
  ['dependency verification is missing declared dependency: ', 'Хамаарлын шалгалтад зарласан хамаарал алга: '],
  ['required dependency is not ready: ', 'Шаардлагатай хамаарал бэлэн биш: '],
  ['approval workflow contract is missing required input: ', 'Баталгааны ажлын урсгалын гэрээнд шаардлагатай оролт алга: '],
  ['approval workflow contract is missing decision output: ', 'Баталгааны ажлын урсгалын гэрээнд шийдвэрийн гаралт алга: '],
  ['approval workflow contract is missing status mapping: ', 'Баталгааны ажлын урсгалын гэрээнд төлөвийн харгалзуулалт алга: '],
  ['maclaw app package is not ready for Hub approval: ', 'MaClaw аппын багц Hub-ын баталгаанд бэлэн биш: '],
];

/** Stable Hub and local publish review sentences. Unknown text stays as sent. English and Chinese are unchanged. */
export function appsReviewIssueText(lang: string | undefined | null, sentence: string | undefined | null): string {
  const raw = String(sentence || '').trim();
  if (!raw || normalizeLang(lang) !== 'mn') return raw;
  const exact = reviewIssueExact[raw];
  if (exact) return exact;
  for (const [prefix, mn] of reviewIssuePrefixes) {
    if (raw.startsWith(prefix)) return `${mn}${raw.slice(prefix.length)}`;
  }
  let match = /^approval workflow Skill (.+) is installed but not active$/.exec(raw);
  if (match) return `Баталгааны ажлын урсгалын Skill ${match[1]} суусан боловч идэвхгүй`;
  match = /^approval workflow Skill (.+) version (.+) does not match required (.+)$/.exec(raw);
  if (match) return `Баталгааны ажлын урсгалын Skill ${match[1]}-ийн ${match[2]} хувилбар шаардлагатай ${match[3]}-тай таарахгүй`;
  // Reuse an existing exact catalog sentence. Do not apply ${} templates.
  return mnExactSentence(raw) || raw;
}

/** Mongolian sentence for UI that used to pick Chinese or English only. */
export function appsLocalized(lang: string | undefined | null, en: string, zhHans: string, mn: string, zhHant?: string): string {
  if (normalizeLang(lang) === 'mn') return mn;
  return localizeText(lang, en, zhHans, zhHant);
}
