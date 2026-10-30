/^diff --git / { header = 1; next }
header && /^\+\+\+ / { file = substr($0, 7); next }
/^@@/ {
  header = 0
  split($3, range, ",")
  line = substr(range[1], 2) + 0
  next
}
!header && /^\+/ {
  print file ":" line ":" substr($0, 2)
  line++
}
