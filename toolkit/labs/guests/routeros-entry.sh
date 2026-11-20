#!/bin/sh
LAB=/lab
LAB_ROS_FIREWALL=legacy
[ -f "$LAB/env" ] && . "$LAB/env"
tries=0
until apk add -q openssh-server > /dev/null 2>&1; do
	tries=$((tries + 1))
	[ "$tries" -lt 60 ] || break
	sleep 5
done
if [ -x /usr/sbin/sshd ]; then
	sed -i 's/^root:!:/root:*:/' /etc/shadow
	mkdir -p /root/.ssh /root/lab
	cp "$LAB/authorized_keys" /root/.ssh/authorized_keys
	chmod 700 /root/.ssh
	chmod 600 /root/.ssh/authorized_keys
	ssh-keygen -A > /dev/null
	/usr/sbin/sshd -o PermitRootLogin=prohibit-password -o PasswordAuthentication=no
else
	echo "lab: openssh-server did not install, so the lab cannot reach this container" >&2
fi
if [ "$LAB_ROS_FIREWALL" = legacy ]; then
	sed -i "s/^lsmod | grep -q '^nf_tables' && NFT_CORE=1 || NFT_CORE=0\$/NFT_CORE=0/" /entrypoint.sh
fi
exec /entrypoint.sh
