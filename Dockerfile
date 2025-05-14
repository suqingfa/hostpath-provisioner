FROM debian:12
COPY "hostpath-provisioner" "/hostpath-provisioner"
CMD ["/hostpath-provisioiner"]
