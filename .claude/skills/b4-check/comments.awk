{
  if (!match($0, /^[^:]*:[0-9]+:/)) next
  path = substr($0, 1, RLENGTH - 1)
  sub(/:[0-9]+$/, "", path)
  text = substr($0, RLENGTH + 1)
}
path ~ /\.(go|ts|tsx|js|jsx|mjs|cjs)$/ {
  if (text ~ /^[[:space:]]*\/\/go:/) next
  if (text ~ /(^|[[:space:]{(])\/[\/*]/) print
  next
}
path ~ /(\.sh|\.ya?ml)$/ || path ~ /(^|\/)(Makefile|Dockerfile)$/ {
  if (text ~ /^#!/) next
  if (text ~ /^[[:space:]]*#/) print
}
