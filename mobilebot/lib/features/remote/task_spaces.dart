import 'remote_models.dart';

/// 「空间→任务」两级分组（WorkBuddy 风格）：任务按所在目录聚成空间，
/// 空间头可折叠。纯函数、无 Flutter 依赖，方便单测。
///
/// 分组键：优先 `working_dir`（新建任务时显式选择的工作目录），缺失时
/// 退化为 `project_path` 的父目录（老数据只有项目路径）。两者都空 →
/// 未分组空间（key 为空串），展示层给一个「其他」标题。
class TaskSpace {
  /// 归一化后的目录路径；未分组任务用空串。
  final String key;

  /// 空间标题 — 目录 basename；未分组时为空串，由 UI 走本地化文案。
  final String label;

  /// 完整目录路径，供副标题/提示展示。
  final String fullPath;

  /// 组内任务，已按 pinned → lastActivity 排好序。
  final List<RemoteTask> tasks;

  const TaskSpace({
    required this.key,
    required this.label,
    required this.fullPath,
    required this.tasks,
  });

  bool get isUngrouped => key.isEmpty;
  bool get running => tasks.any((t) => t.running);

  /// 运行中任务数 — 空间头右侧的状态徽章；0 时不显示。
  int get runningCount => tasks.where((t) => t.running).length;

  /// 组内最新活动时间；无活动的任务 lastActivity 为空串，不影响比较。
  String get lastActivity {
    var latest = '';
    for (final t in tasks) {
      if (t.lastActivity.compareTo(latest) > 0) latest = t.lastActivity;
    }
    return latest;
  }
}

/// 把扁平任务列表归成空间：running 空间排最前，其余按最近活动倒序。
List<TaskSpace> groupTasksIntoSpaces(List<RemoteTask> tasks) {
  final byDir = <String, List<RemoteTask>>{};
  final order = <String>[];
  for (final task in tasks) {
    final key = _spaceKey(task);
    if (!byDir.containsKey(key)) {
      byDir[key] = [];
      order.add(key);
    }
    byDir[key]!.add(task);
  }

  final spaces = [
    for (final key in order)
      TaskSpace(
        key: key,
        label: key.isEmpty ? '' : pathBaseName(key),
        fullPath: key,
        tasks: sortedTasks(byDir[key]!),
      ),
  ];

  spaces.sort((a, b) {
    // 运行中的空间浮到顶部，其余按空间内最新活动倒序；
    // 未分组空间不特殊处理，跟着 lastActivity 一起排。
    if (a.running != b.running) return a.running ? -1 : 1;
    return b.lastActivity.compareTo(a.lastActivity);
  });
  return spaces;
}

/// 空间内排序：置顶优先，然后 lastActivity 倒序（与旧扁平列表一致）。
List<RemoteTask> sortedTasks(List<RemoteTask> tasks) {
  final sorted = [...tasks];
  sorted.sort((a, b) {
    if (a.pinned != b.pinned) return a.pinned ? -1 : 1;
    return b.lastActivity.compareTo(a.lastActivity);
  });
  return sorted;
}

/// 任务归属的目录：working_dir 优先，退化到 project_path 的父目录。
/// 路径可能带 / 或 \（受控机可能是 Windows 或 Linux），两种分隔符都吃。
String _spaceKey(RemoteTask task) {
  final workingDir = task.workingDir.trim();
  if (workingDir.isNotEmpty) return normalizeDir(workingDir);
  return parentDir(task.projectPath.isNotEmpty ? task.projectPath : task.id);
}

/// 归一化目录：去首尾空白和结尾分隔符；根目录（"/"、"C:\"）原样保留。
String normalizeDir(String dir) {
  var d = dir.trim();
  while (d.length > 1 && (d.endsWith('/') || d.endsWith('\\'))) {
    // "C:\" 这种盘符根不能把最后的 \ 也削掉。
    if (d.length == 3 && d.endsWith('\\') && d[1] == ':') break;
    d = d.substring(0, d.length - 1);
  }
  return d;
}

/// 父目录；无分隔符或已在根时返回空串（归入未分组）。
String parentDir(String path) {
  final p = normalizeDir(path);
  // 找最后一个 / 或 \。
  var idx = -1;
  for (var i = p.length - 1; i >= 0; i--) {
    if (p[i] == '/' || p[i] == '\\') {
      idx = i;
      break;
    }
  }
  if (idx <= 0) return '';
  // "C:\x" 的父是 "C:\"；"/x" 的父是 "/"（idx==0 已过滤，不会到这）。
  final parent = p.substring(0, idx);
  if (parent.length == 2 && parent[1] == ':') return '$parent\\';
  return parent;
}

/// 目录 basename："D:\proj\web" → "web"；根目录返回原样。
String pathBaseName(String dir) {
  final d = normalizeDir(dir);
  if (d.isEmpty) return '';
  var start = 0;
  for (var i = d.length - 1; i >= 0; i--) {
    if (d[i] == '/' || d[i] == '\\') {
      start = i + 1;
      break;
    }
  }
  final name = d.substring(start);
  return name.isEmpty ? d : name;
}
