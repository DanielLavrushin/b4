function flush() {
  if (mode == "shape" && (c > 0 || sec != "")) print "  " (sec == "" ? "(no section)" : sec) " " c
  sec = ""
  c = 0
}
function problem(msg) {
  print FILENAME ":" FNR ": " msg
}
BEGIN {
  m["Added"] = "added"; m["Добавлено"] = "added"
  m["Changed"] = "changed"; m["Изменено"] = "changed"
  m["Deprecated"] = "deprecated"; m["Устарело"] = "deprecated"
  m["Removed"] = "removed"; m["Удалено"] = "removed"
  m["Fixed"] = "fixed"; m["Исправлено"] = "fixed"
  m["Security"] = "security"; m["Безопасность"] = "security"
  rank["added"] = 1; rank["changed"] = 2; rank["deprecated"] = 3
  rank["removed"] = 4; rank["fixed"] = 5; rank["security"] = 6
  date = " - [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$"
}
FNR == 1 { flush(); seen_version = 0; last = 0 }
/^## / {
  flush()
  seen_version = 1
  last = 0
  if (mode == "shape") { print; next }
  if ($0 == "## [Unreleased]") {
    if (hub) problem("the hub changelog has no [Unreleased] section; its release workflow reads the top block")
    next
  }
  if ($0 !~ ("^## \\[[0-9]+\\.[0-9]+\\.[0-9]+\\]" date)) problem("version heading is not '## [x.y.z] - YYYY-MM-DD': " $0)
  next
}
/^### / {
  flush()
  s = substr($0, 5)
  sec = (s in m) ? m[s] : "unknown:" s
  if (mode == "lint") {
    if (!(s in m)) problem("unknown section heading: " $0)
    else if (rank[sec] <= last) problem("section out of order or repeated (Added, Changed, Deprecated, Removed, Fixed, Security): " $0)
    else last = rank[sec]
  }
  next
}
/^- / {
  c++
  if (mode == "lint") {
    if (seen_version && sec == "") problem("bullet outside a ### section")
    if ($0 ~ /^- (ADDED|FIXED|CHANGED|IMPROVED|REMOVED|ДОБАВЛЕНО|ИСПРАВЛЕНО|ИЗМЕНЕНО|УЛУЧШЕНО|УДАЛЕНО):/) problem("old type prefix on a bullet; the ### section carries the type")
  }
}
END { flush() }
