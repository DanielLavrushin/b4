function hit(kind, why) {
  print kind "\t" why "\t" $0
}
{
  if (!match($0, /^[^:]*:[0-9]+:/)) next
  path = substr($0, 1, RLENGTH - 1)
  sub(/:[0-9]+$/, "", path)
  text = substr($0, RLENGTH + 1)
}
path !~ /^installer\// || path ~ /^installer\/_(build|watch)\.sh$/ { next }
text ~ /grep[[:space:]]+(-[[:alnum:]]*P|--perl)/ { hit("FAIL", "grep -P is GNU only") }
text ~ /\\[sdSD]/ { hit("FAIL", "\\s and \\d are not POSIX: use [[:space:]] and [[:digit:]]") }
text ~ /(^|[[:space:];|&(`])(od|hexdump|xxd)([[:space:]]|$)/ { hit("FAIL", "Merlin's busybox has no od: read bytes with _byte_at") }
text ~ /(^|[[:space:];&|(])\[\[[[:space:]]/ { hit("FAIL", "[[ is a bashism: use [ ]") }
text ~ /(^|[[:space:];])function[[:space:]]+[A-Za-z_]/ { hit("FAIL", "the function keyword is a bashism: name() {") }
text ~ /<<</ { hit("FAIL", "here-strings are a bashism") }
text ~ /\$'/ { hit("FAIL", "$'...' quoting is a bashism: use printf") }
text ~ /&>/ { hit("FAIL", "&> is a bashism: use >file 2>&1") }
text ~ /(^|[[:space:];])source[[:space:]]/ { hit("FAIL", "source is a bashism: use .") }
text ~ /\$\{[A-Za-z_][A-Za-z0-9_]*:[0-9]/ { hit("FAIL", "${var:offset} is a bashism") }
text ~ /\$\{[A-Za-z_][A-Za-z0-9_]*(\/|\^|,)/ { hit("FAIL", "${var/a/b} and case modifiers are bashisms") }
text ~ /(^|[[:space:];])(declare|typeset|readarray|mapfile|shopt|let)[[:space:]]/ { hit("FAIL", "bash builtin") }
text ~ /(^|[[:space:];])[A-Za-z_][A-Za-z0-9_]*=\(/ { hit("FAIL", "arrays are a bashism") }
text ~ /(^|[[:space:];])\(\(/ { hit("FAIL", "(( )) is a bashism: use $(( )) or [ ]") }
text ~ /\[[[:space:]][^]]*[[:space:]]==[[:space:]]/ { hit("FAIL", "== inside [ ] is not POSIX: use =") }
text ~ /echo[[:space:]]+-e/ { hit("FAIL", "echo -e is not portable: use printf") }
text ~ /(^|[[:space:];])local[[:space:]]+[A-Za-z_]/ { hit("FAIL", "local is not POSIX: the installer uses prefixed globals such as _ba_char") }
path != "installer/lib/utils.sh" && text ~ /command[[:space:]]+-v/ { hit("NOTE", "probe tools with command_exists from installer/lib/utils.sh") }
