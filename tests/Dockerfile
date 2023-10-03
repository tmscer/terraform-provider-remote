FROM alpine:latest

COPY key.pub /root/.ssh/authorized_keys

RUN apk add --no-cache \
        bash \
        openssh \
        sudo \
    && ssh-keygen -A \
    && sed -i "s/#\?PermitRootLogin.*/PermitRootLogin yes/" /etc/ssh/sshd_config \
    && sed -i "s/#\?AllowTcpForwarding.*/AllowTcpForwarding yes/" /etc/ssh/sshd_config \
    && cp /etc/ssh/sshd_config /etc/ssh/sshd_config_unexposed \
    && sed -i "s/^#\?Port.*/Port 1022/" /etc/ssh/sshd_config_unexposed \
    && adduser -D bob \
    && echo "root:password" | chpasswd \
    && echo "bob:pwd" | chpasswd \
    && chmod 600 /root/.ssh/authorized_keys

EXPOSE 22
CMD ["bash", "-c", "/usr/sbin/sshd -D & /usr/sbin/sshd -f /etc/ssh/sshd_config_unexposed -D"]
