import { normalizeLang } from './langSelect';

const botsMnByEnglish: Record<string, string> = {
  "Bots": "Bots",
  "No bots yet.": "Одоогоор Bot алга.",
  "Edit info": "Мэдээлэл засах",
  "Character and voice": "Зан төлөв ба ярианы хэв",
  "Edit bot info": "Bot-ын мэдээллийг засах",
  "Delete bot": "Bot устгах",
  "Delete bot \"${name}\"? This cannot be undone.": "Bot «${name}»-г устгах уу? Үүнийг буцаах боломжгүй.",
  "Color": "Өнгө",
  "Shape": "Хэлбэр",
  "Another bot already has this name": "Ийм нэртэй өөр Bot байна",
  "New bot": "Шинэ Bot",
  "Persona": "Дүр",
  "No persona yet.": "Одоогоор дүр алга.",
  "A 24/7 digital assistant on the remote computer.": "Алсын компьютер дээрх 24/7 тоон туслах.",
  "Select a bot to send a task.": "Даалгавар илгээхийн тулд Bot сонгоно уу.",
  "No conversation yet — send the first task below.": "Харилцан яриа одоогоор алга — доор эхний даалгавраа илгээнэ үү.",
  "Message ${name}": "${name}-д зурвас илгээх",
  "Details": "Дэлгэрэнгүй",
  "Set as primary bot": "Үндсэн Bot болгох",
  "Already the primary bot": "Аль хэдийн үндсэн Bot",
  "Computer": "Компьютер",
  "Created": "Үүсгэсэн",
  "Credits available": "Боломжит Credits",
  "Credits remaining": "Үлдсэн Credits",
  "Your credits are nearly used up. A task may stop partway through.": "Таны Credits бараг дууссан. Даалгавар дундуур зогсож магадгүй.",
  "Your credits are used up. A task will stop.": "Таны Credits дууссан. Даалгавар зогсоно.",
  "Top up": "Цэнэглэх",
  "Change settings": "Тохиргоо өөрчлөх",
  "${name}'s screen": "${name}-н дэлгэц",
  "Desktop screenshot": "Ширээний дэлгэцийн зураг",
  "View full size": "Бүтэн хэмжээгээр харах",
  "working": "ажиллаж байна",
  "arranging": "зохицуулж байна",
  "idle": "хүлээлтэд",
  "Arranging": "Зохицуулж байна",
  "Run this arrangement": "Энэ зохицуулалтаар ажиллуулах",
  "Use the password box below. Do not paste it into the chat.": "Доорх нууц үгийн талбарыг ашиглана уу. Чатад буулгаж болохгүй.",
  "Retry connection": "Холболтыг дахин оролдох",
  "The desktop picture did not connect. Retry the connection — the task is not sent again.": "Ширээний зураг холбогдсонгүй. Холболтыг дахин оролдоно уу — даалгаврыг дахин илгээхгүй.",
  "That password field cannot be filled from here. Open this bot’s computer and type it yourself.": "Энэ нууц үгийн талбарыг эндээс бөглөх боломжгүй. Энэ Bot-ын компьютерыг нээж, өөрөө бичнэ үү.",
  "The password was not saved on this computer.": "Нууц үг энэ компьютер дээр хадгалагдаагүй.",
  "Hide desktop": "Ширээг нуух",
  "Take over": "Хяналт авах",
  "Take over the computer": "Компьютерийн хяналтыг авах",
  "Take the keyboard and finish the login or check on this computer.": "Гарыг аваад энэ компьютер дээр нэвтрэлт эсвэл баталгаажуулалтыг дуусгана уу.",
  "Release": "Чөлөөлөх",
  "Demo → skill": "Үзүүлэн → Skill",
  "Start recording": "Бичлэг эхлүүлэх",
  "Stop recording": "Бичлэг зогсоох",
  "Narrate": "Тайлбар",
  "Push to talk": "Дараад ярих",
  "Distill skill": "Skill болгон нягтруулах",
  "Save edits": "Засварыг хадгалах",
  "Re-distill": "Дахин нягтруулах",
  "Demo title (optional)": "Үзүүлэнгийн гарчиг (заавал биш)",
  "What this step is doing": "Энэ алхам юу хийж байгаа",
  "Fullscreen the desktop and take the keyboard from the bot.": "Ширээг бүтэн дэлгэцэд гаргаж, гарыг Bot-оос авна.",
  "This bot is using its desktop. Take over to stop it and take the keyboard.": "Энэ Bot өөрийн ширээг ашиглаж байна. Зогсоогоод гарыг авахын тулд хяналтыг авна уу.",
  "The bot is using the desktop. Stop it first if you need the computer.": "Bot ширээг ашиглаж байна. Компьютер хэрэгтэй бол эхлээд зогсооно уу.",
  "Stop": "Зогсоох",
  "You have the desktop. Release it to hand the keyboard back.": "Ширээ танд байна. Гарыг буцааж өгөхийн тулд чөлөөлнө үү.",
  "My desktop": "Миний ширээ",
  "Shared": "Хуваалцсан",
  "Desktops": "Ширээнүүд",
  "Hand back to the bot": "Bot-д буцааж өгөх",
  "Handing the desktop back…": "Ширээг буцааж өгч байна…",
  "This bot needs you on its desktop.": "Энэ Bot таныг өөрийнхөө ширээн дээр байхыг шаардаж байна.",
  "The desktop was not handed back.": "Ширээг буцааж өгсөнгүй.",
  "No bot is sharing your desktop.": "Таны ширээг хуваалцаж буй Bot алга.",
  "Own desktop": "Өөрийн ширээ",
  "This bot uses its own virtual desktop. Off means it shares yours.": "Энэ Bot өөрийн виртуал ширээг ашиглана. Унтраавал таныхыг хуваалцана.",
  "The desktop mode was not changed.": "Ширээний горим өөрчлөгдсөнгүй.",
  "Watching the desktop. It stays open while you watch.": "Ширээг үзэж байна. Үзэж байх үед нээлттэй хэвээр байна.",
  "Take control": "Удирдлага авах",
  "Screen mode": "Дэлгэцийн горим",
  "H.264": "H.264",
  "VNC": "VNC",
  "Switch the desktop player: H.264 (smooth, low latency) or VNC (fallback).": "Ширээний тоглуулагчийг солих: H.264 (жигд, бага хоцрогдол) эсвэл VNC (нөөц).",
  "The bot handed you the keyboard — finish the login, then press \"Login done, continue\".": "Bot танд гарыг өгсөн — нэвтрэлтийг дуусгаад «Нэвтрэлт дууссан, үргэлжлүүлэх» дээр дарна уу.",
  "Starting the desktop…": "Ширээг эхлүүлж байна…",
  "Enter to send, Shift+Enter for a new line, ↑↓ for history.": "Илгээхэд Enter, шинэ мөрөнд Shift+Enter, түүхэнд ↑↓.",
  "Resize bot list": "Bot жагсаалтын хэмжээг өөрчлөх",
  "Drag to resize. Double-click to reset.": "Чирж хэмжээг өөрчилнө. Хоёр товшоод анхны хэмжээнд буцаана.",
  "Drag to move. Click to open the desktop.": "Зөөхийн тулд чирнэ үү. Ширээг нээхийн тулд дарна уу.",
  "The bot is using the desktop.": "Bot ширээг ашиглаж байна.",
  "The website login is still in this browser. Send another message and this bot continues there.": "Вэбсайтын нэвтрэлт энэ хөтөч дээр хэвээр байна. Дахиад зурвас илгээвэл энэ Bot тэндээс үргэлжлүүлнэ.",
  "Finish the login or verification in this browser. The login stays here for the bot.": "Энэ хөтөч дээр нэвтрэлт эсвэл баталгаажуулалтыг дуусгана уу. Нэвтрэлт Bot-д зориулж энд үлдэнэ.",
  "Login done, continue": "Нэвтрэлт дууссан, үргэлжлүүлэх",
  "The backend agent is unavailable.": "Арын Agent боломжгүй.",
  "That message did not become an order, so it was not sent.": "Энэ зурвас захиалга болсонгүй тул илгээгдсөнгүй.",
  "That message did not become a schedule, so none was set.": "Энэ зурвас хуваарь болсонгүй тул юу ч тохируулаагүй.",
  "This one had not gone out yet when the app restarted. You can send your own words as they are, or have me read it again.": "Апп дахин эхлэхэд энэ зурвас хараахан гараагүй байсан. Өөрийнхөө үгийг хэвээр нь илгээж болно, эсвэл надаар дахин уншуулж болно.",
  "I could not turn that into an arrangement — the service that reads your messages did not answer. You can send your own words as they are, or have me read it again.": "Би үүнийг зохицуулалт болгож чадсангүй — таны зурвасыг уншдаг үйлчилгээ хариулаагүй. Өөрийнхөө үгийг хэвээр нь илгээж болно, эсвэл надаар дахин уншуулж болно.",
  "Read it again": "Дахин унших",
  "Send my own words": "Үгээ хэвээр илгээх",
  "No matching schedule was found.": "Тохирох хуваарь олдсонгүй.",
  "The schedule is set. The result will be reported here.": "Хуваарь тохируулагдлаа. Үр дүнг энд мэдэгдэнэ.",
  "The schedule is stopped.": "Хуваарь зогссон.",
  "Schedules": "Хуваарь",
  "No ongoing tasks.": "Үргэлжилж буй даалгавар алга.",
  "Queued": "Дараалалд",
  "Waiting for you": "Таныг хүлээж байна",
  "Possibly interrupted": "Тасалдсан байж магадгүй",
  "Started": "Эхэлсэн",
  "Stopped.": "Зогссон.",
  "Cancelled.": "Цуцлагдсан.",
  "Not tracked by this app; its process may be gone. Cancel closes the record.": "Энэ апп үүнийг хянахгүй байна; процесс нь алга болсон байж магадгүй. Цуцлахад бүртгэл хаагдана.",
  "Untitled run": "Нэргүй ажиллагаа",
  "No schedules yet.": "Одоогоор хуваарь алга.",
  "Delete schedule": "Хуваарь устгах",
  "Delete schedule \"${name}\"?": "«${name}» хуваарийг устгах уу?",
  "This message was not sent. The account changed before it went out.": "Энэ зурвас илгээгдсөнгүй. Гарахаас өмнө данс солигдсон.",
  "Model and network settings for these bots": "Эдгээр Bot-уудын загвар ба сүлжээний тохиргоо",
  "Network": "Сүлжээ",
  "Cloud desktop network": "Үүлэн ширээний сүлжээ",
  "How the cloud desktop reaches websites. Switching applies right away; the desktop does not restart and logins stay.": "Үүлэн ширээ вэбсайтад хэрхэн хандах. Солих нь шууд үйлчилнэ; ширээ дахин эхлэхгүй, нэвтрэлт хэвээр үлдэнэ.",
  "Current: ${mode}": "Одоогийн: ${mode}",
  "Proxy": "Прокси",
  "Every site goes through the overseas proxy.": "Бүх сайт гадаад проксигоор дамжина.",
  "Direct": "Шууд",
  "Every site is reached directly from the desktop host.": "Бүх сайтад ширээний хостоос шууд хандана.",
  "Smart": "Ухаалаг",
  "Mainland China sites (JD, Taobao, Baidu…) go direct, everything else through the proxy. Recommended.": "Эх газрын Хятадын сайтууд (JD, Taobao, Baidu…) шууд, бусад нь проксигоор дамжина. Зөвлөж байна.",
  "Your administrator fixed this setting for the organization, so it cannot be changed here.": "Админ энэ тохиргоог байгууллагад тогтоосон тул энд өөрчлөх боломжгүй.",
  "Cloud computer": "Үүлэн компьютер",
  "You and all of your bots share this one computer. Restarting or resetting it affects every bot at the same time.": "Та болон таны бүх Bot энэ компьютерыг хуваалцана. Дахин эхлүүлэх эсвэл дахин тохируулах нь бүх Bot-д зэрэг нөлөөлнө.",
  "Restart computer": "Компьютер дахин эхлүүлэх",
  "Like turning a computer off and on again.": "Компьютерийг унтраагаад дахин асаасантай адил.",
  "Open windows close and tasks bots are running are interrupted.": "Нээлттэй цонхнууд хаагдаж, Bot-уудын ажиллаж буй даалгавар тасалдана.",
  "Your files, installed software and website logins are all kept (logins are saved before shutting down).": "Таны файлууд, суулгасан програм хангамж, вэбсайтын нэвтрэлт бүгд хадгалагдана (унтраахаас өмнө нэвтрэлтийг хадгална).",
  "Takes about 1 minute.": "Ойролцоогоор 1 минут болно.",
  "Try this first when the computer is slow or the browser misbehaves.": "Компьютер удаан байвал, эсвэл хөтөч хэвийн бус ажиллавал эхлээд үүнийг туршина уу.",
  "Restart the cloud computer?": "Үүлэн компьютерыг дахин эхлүүлэх үү?",
  "Interrupt and restart": "Таслаад дахин эхлүүлэх",
  "Restarting… saving website logins, then starting again. Please keep this window open.": "Дахин эхлүүлж байна… вэбсайтын нэвтрэлтийг хадгалаад дахин асаана. Энэ цонхыг нээлттэй үлдээнэ үү.",
  "Restarted. The browser opens again the next time you or a bot uses the computer.": "Дахин эхэллээ. Та эсвэл Bot дараагийн удаа компьютер ашиглахад хөтөч дахин нээгдэнэ.",
  "Reset computer": "Компьютер дахин тохируулах",
  "Like installing a fresh computer with the latest system, then moving your data back.": "Хамгийн сүүлийн системээр компьютерийг шинээр суулгаад, өгөгдлийг буцааж зөөсөнтэй адил.",
  "Kept: website logins and browser data (yours and your bots'), and the files in your home folder.": "Хадгалагдана: вэбсайтын нэвтрэлт ба хөтчийн өгөгдөл (таны болон таны Bot-уудын), мөн гэрийн хавтас дахь файлууд.",
  "Not kept: all software you installed yourself and all system settings. The computer returns to its factory state. (The administrator keeps one backup of the old software.)": "Хадгалагдахгүй: өөрөө суулгасан бүх програм хангамж ба системийн бүх тохиргоо. Компьютер үйлдвэрийн төлөвтөө буцна. (Админ хуучин програм хангамжийн нэг нөөц хуулбарыг хадгална.)",
  "Takes about 1–3 minutes (longer if a new system has to be downloaded).": "Ойролцоогоор 1–3 минут болно (шинэ систем татах бол илүү удаан).",
  "Use this only when restarting does not fix the problem.": "Дахин эхлүүлэх нь асуудлыг засахгүй үед л үүнийг ашиглана уу.",
  "Reset the cloud computer?": "Үүлэн компьютерыг дахин тохируулах уу?",
  "I understand all software I installed and system settings will be removed (factory state)": "Өөрөө суулгасан бүх програм хангамж, системийн тохиргоо устахыг ойлгож байна (үйлдвэрийн төлөв)",
  "Interrupt and reset": "Таслаад дахин тохируулах",
  "Resetting… saving website logins, restoring factory state and moving your data back. Please keep this window open.": "Дахин тохируулж байна… вэбсайтын нэвтрэлтийг хадгалаад, үйлдвэрийн төлөвийг сэргээж, өгөгдлийг буцааж зөөнө. Энэ цонхыг нээлттэй үлдээнэ үү.",
  "Reset finished. Website logins and home-folder files are in place; the computer is back to factory state.": "Дахин тохируулж дууслаа. Вэбсайтын нэвтрэлт ба гэрийн хавтасны файлууд байрандаа; компьютер үйлдвэрийн төлөвтөө буцсан.",
  "Your administrator has turned off reset for this organization.": "Админ энэ байгууллагад дахин тохируулахыг унтраасан.",
  "${count} bot task(s) are running. Continuing will interrupt them.": "${count} Bot-ын даалгавар ажиллаж байна. Үргэлжлүүлбэл тэдгээр тасалдана.",
  "A bot is using the computer for a task right now. Continuing will interrupt that task.": "Нэг Bot одоо энэ компьютер дээр даалгавар гүйцэтгэж байна. Үргэлжлүүлбэл тэр даалгавар тасалдана.",
  "Model for your bots": "Таны Bot-уудын загвар",
  "Every bot of this account runs on the same model. Changing it here moves all of them.": "Энэ дансны бүх Bot нэг загвар дээр ажиллана. Энд өөрчилбөл бүгд хамт шилжинэ.",
  "Hand the bots back to whatever the service resolves for you — either your own route, or the provider an administrator picked.": "Bot-уудыг үйлчилгээний шийдэлд буцаана — таны өөрийн шугам, эсвэл админы сонгосон үйлчилгээ үзүүлэгч.",
  "Pick a provider from this computer": "Энэ компьютерээс үйлчилгээ үзүүлэгч сонгох",
  "Provider": "Үйлчилгээ үзүүлэгч",
  "Loading the model list…": "Загварын жагсаалтыг ачаалж байна…",
  "This provider has no models to pick from yet.": "Энэ үйлчилгээ үзүүлэгчид одоогоор сонгох загвар алга.",
  "No provider on this computer has a key yet. Add one under Settings → Providers first.": "Энэ компьютер дээр одоогоор түлхүүртэй үйлчилгээ үзүүлэгч алга. Эхлээд Тохиргоо → Үйлчилгээ үзүүлэгч хэсэгт нэгийг нэмнэ үү.",
  "Using your own model: ${detail}": "Өөрийн загварыг ашиглаж байна: ${detail}",
  "Using your own model": "Өөрийн загварыг ашиглаж байна",
};

type BotTemplate = { re: RegExp; literals: number; mnParts: string[] };
let compiled: BotTemplate[] | null = null;

function splitHoles(input: string): string[] | null {
  const parts: string[] = [];
  let literal = '';
  let holes = 0;
  for (let i = 0; i < input.length; i++) {
    if (input.startsWith('${', i)) {
      const j = input.indexOf('}', i + 2);
      if (j < 0) return null;
      parts.push(literal);
      literal = '';
      holes += 1;
      i = j;
      continue;
    }
    literal += input[i];
  }
  parts.push(literal);
  return holes > 0 ? parts : null;
}

function escapeLiteral(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

export function botsMn(lang: string | undefined | null, en: string): string {
  if (normalizeLang(lang) !== 'mn') return en;
  const exact = botsMnByEnglish[en];
  if (exact) return exact;
  if (!compiled) {
    compiled = [];
    for (const key of Object.keys(botsMnByEnglish)) {
      const parts = splitHoles(key);
      if (!parts) continue;
      const mnParts = splitHoles(botsMnByEnglish[key]);
      if (!mnParts || mnParts.length !== parts.length) continue;
      compiled.push({
        re: new RegExp('^' + parts.map(escapeLiteral).join('([\\s\\S]*?)') + '$'),
        literals: parts.reduce((sum, part) => sum + part.length, 0),
        mnParts,
      });
    }
    compiled.sort((a, b) => b.literals - a.literals);
  }
  for (const template of compiled) {
    const match = template.re.exec(en);
    if (!match) continue;
    let out = template.mnParts[0];
    for (let i = 1; i < template.mnParts.length; i++) out += (match[i] ?? '') + template.mnParts[i];
    return out;
  }
  return en;
}
