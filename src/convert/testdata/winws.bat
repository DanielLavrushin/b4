@echo off
set BIN=%~dp0..\bin\
rem HTTPS strategy for the general list
start "zapret: general" /min "%BIN%winws.exe" ^
--wf-tcp=80,443 --wf-udp=443,50000-50100 ^
--filter-tcp=80 --dpi-desync=fake,split2 --dpi-desync-autottl=2 --dpi-desync-fooling=md5sig --new ^
--filter-tcp=443 --dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld --dpi-desync-repeats=6 --new ^
--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11
