# CIS Ubuntu 24.04 — Degraded items 운영자 가이드

> 생성: 2026-05-13 06:28 UTC · 자동 변환 안 된 항목들의 audit·remediation 정리.
> 운영자가 각 항목을 수동으로 검토하거나 customer 환경 fixture로 customizing할 때 활용.

**통계**: 총 70건 degraded (Manual: 21 / NoMarker: 49).

---

## Manual review (assessment_status=Manual, 21건)

CIS 가이드가 명시적으로 manual review를 요구한 항목들. 자동 변환 불가능 — 운영자가 customer 환경 정책에 따라 직접 검증.

### 1.1.1.10 — 1.1.1.10 Ensure unused filesystems kernel modules are not (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Filesystem kernel modules are pieces of code that can be dynamically loaded into the
Linux kernel to extend its filesystem capabilities, or so-called base kernel, of an
operating system. Filesystem kernel modules are typically used to add support for new
hardware (as device drivers), or for adding system calls.

**Rationale**:

While loadable filesystem kernel modules are a convenient method of modifying the
running kernel, this can be abused by attackers on a compromised system to prevent
detection of their processes or files, allowing them to maintain control over the system.
Many rootkits make use of loadable filesystem kernel modules in this way.
Removing support for unneeded filesystem types reduces the local attack surface of the
system. If this filesystem type is not needed, disable it. The following filesystem kernel
modules have known CVE's and should be made unavailable if no dependencies exist:
• afs - CVE-2022-37402
• ceph - CVE-2022-0670
• cifs - CVE-2022-29869
• exfat CVE-2022-29973
• ext CVE-2022-1184
• fat CVE-2022-22043
• fscache CVE-2022-3630
• fuse CVE-2023-0386
• gfs2 CVE-2023-3212
• nfs_common CVE-2023-6660
• nfsd CVE-2022-43945
• smbfs_common CVE-2022-2585
Impact:
This list may be quite extensive and covering all edges cases is difficult. Therefore, it's
crucial to carefully consider the implications and dependencies before making any
changes to the filesystem kernel module configurations.

**Audit guide**:

```
Run the following script to:
• Look at the filesystem kernel modules available to the currently running kernel.
• Exclude mounted filesystem kernel modules that don't currently have a CVE
• List filesystem kernel modules that are not fully disabled, or are loaded into the
kernel
Review the generated output
#! /usr/bin/env bash
{
a_output=(); a_output2=(); a_modprope_config=(); a_excluded=(); a_available_modules=()
a_ignore=("xfs" "vfat" "ext2" "ext3" "ext4")
a_cve_exists=("afs" "ceph" "cifs" "exfat" "ext" "fat" "fscache" "fuse" "gfs2" "nfs_common"
"nfsd" "smbfs_common")
f_module_chk()
{
l_out2=""; grep -Pq -- "\b$l_mod_name\b" <<< "${a_cve_exists[*]}" && l_out2=" <- CVE
exists!"
if ! grep -Pq -- '\bblacklist\h+'"$l_mod_name"'\b' <<< "${a_modprope_config[*]}"; then
a_output2+=(" - Kernel module: \"$l_mod_name\" is not fully disabled $l_out2")
elif ! grep -Pq -- '\binstall\h+'"$l_mod_name"'\h+(\/usr)?\/bin\/(false|true)\b' <<<
"${a_modprope_config[*]}"; then
a_output2+=(" - Kernel module: \"$l_mod_name\" is not fully disabled $l_out2")
fi
if lsmod | grep "$l_mod_name" &> /dev/null; then # Check if the module is currently loaded
l_output2+=(" - Kernel module: \"$l_mod_name\" is loaded" "")
fi
}
while IFS= read -r -d $'\0' l_module_dir; do
a_available_modules+=("$(basename "$l_module_dir")")
done < <(find "$(readlink -f /lib/modules/"$(uname -r)"/kernel/fs)" -mindepth 1 -maxdepth 1 -
type d ! -empty -print0)
while IFS= read -r l_exclude; do
if grep -Pq -- "\b$l_exclude\b" <<< "${a_cve_exists[*]}"; then
a_output2+=(" - ** WARNING: kernel module: \"$l_exclude\" has a CVE and is currently
mounted! **")
elif
grep -Pq -- "\b$l_exclude\b" <<< "${a_available_modules[*]}"; then
a_output+=(" - Kernel module: \"$l_exclude\" is currently mounted - do NOT unload or
disable")
fi
! grep -Pq -- "\b$l_exclude\b" <<< "${a_ignore[*]}" && a_ignore+=("$l_exclude")
done < <(findmnt -knD | awk '{print $2}' | sort -u)
while IFS= read -r l_config; do
a_modprope_config+=("$l_config")
done < <(modprobe --showconfig | grep -P '^\h*(blacklist|install)')
for l_mod_name in "${a_available_modules[@]}"; do # Iterate over all filesystem modules
[[ "$l_mod_name" =~ overlay ]] && l_mod_name="${l_mod_name::-2}"
if grep -Pq -- "\b$l_mod_name\b" <<< "${a_ignore[*]}"; then
a_excluded+=(" - Kernel module: \"$l_mod_name\"")
else
f_module_chk
fi
done
[ "${#a_excluded[@]}" -gt 0 ] && printf '%s\n' "" " -- INFO --" \
"The following intentionally skipped" \
"${a_excluded[@]}"
if [ "${#a_output2[@]}" -le 0 ]; then
printf '%s\n' "" " - No unused filesystem kernel modules are enabled" "${a_output[@]}" ""
else
printf '%s\n' "" "-- Audit Result: --" " ** REVIEW the following **" "${a_output2[@]}"
[ "${#a_output[@]}" -gt 0 ] && printf '%s\n' "" "-- Correctly set: --" "${a_output[@]}" ""
fi
}
WARNING: disabling or denylisting filesystem modules that are in use on the system
may be FATAL. It is extremely important to thoroughly review this list.
```

**Remediation**:

```
- IF - the module is available in the running kernel:
• Unload the filesystem kernel module from the kernel
• Create a file ending in .conf with install filesystem kernel modules /bin/false
in the /etc/modprobe.d/ directory
• Create a file ending in .conf with deny list filesystem kernel modules in the
/etc/modprobe.d/ directory
WARNING: unloading, disabling or denylisting filesystem modules that are in use on the
system maybe FATAL. It is extremely important to thoroughly review the filesystems
returned by the audit before following the remediation procedure.
Example of unloading the gfs2kernel module:
# modprobe -r gfs2 2>/dev/null
# rmmod gfs2 2>/dev/null
Example of fully disabling the gfs2 kernel module:
# printf '%s\n' "blacklist gfs2" "install gfs2 /bin/false" >>
/etc/modprobe.d/gfs2.conf
Note:
• Disabling a kernel module by modifying the command above for each unused
filesystem kernel module
• The example gfs2 must be updated with the appropriate module name for the
command or example script bellow to run correctly.
Below is an example Script that can be modified to use on various filesystem
kernel modules manual remediation process:
Example Script
#!/usr/bin/env bash
{
a_output2=(); a_output3=(); l_dl="" # Initialize arrays and clear
variables
l_mod_name="gfs2" # set module name
l_mod_type="fs" # set module type
l_mod_path="$(readlink -f /lib/modules/**/kernel/$l_mod_type | sort -u)"
f_module_fix()
{
l_dl="y" # Set to ignore duplicate checks
a_showconfig=() # Create array with modprobe output
while IFS= read -r l_showconfig; do
a_showconfig+=("$l_showconfig")
done < <(modprobe --showconfig | grep -P --
'\b(install|blacklist)\h+'"${l_mod_name//-/_}"'\b')
if lsmod | grep "$l_mod_name" &> /dev/null; then # Check if the module
is currently loaded
a_output2+=(" - unloading kernel module: \"$l_mod_name\"")
modprobe -r "$l_mod_name" 2>/dev/null; rmmod "$l_mod_name"
2>/dev/null
fi
if ! grep -Pq -- '\binstall\h+'"${l_mod_name//-
/_}"'\h+(\/usr)?\/bin\/(true|false)\b' <<< "${a_showconfig[*]}"; then
a_output2+=(" - setting kernel module: \"$l_mod_name\" to
\"$(readlink -f /bin/false)\"")
printf '%s\n' "install $l_mod_name $(readlink -f /bin/false)" >>
/etc/modprobe.d/"$l_mod_name".conf
fi
if ! grep -Pq -- '\bblacklist\h+'"${l_mod_name//-/_}"'\b' <<<
"${a_showconfig[*]}"; then
a_output2+=(" - denylisting kernel module: \"$l_mod_name\"")
printf '%s\n' "blacklist $l_mod_name" >>
/etc/modprobe.d/"$l_mod_name".conf
fi
}
for l_mod_base_directory in $l_mod_path; do # Check if the module exists
on the system
if [ -d "$l_mod_base_directory/${l_mod_name/-/\/}" ] && [ -n "$(ls -A
"$l_mod_base_directory/${l_mod_name/-/\/}")" ]; then
a_output3+=(" - \"$l_mod_base_directory\"")
[[ "$l_mod_name" =~ overlay ]] && l_mod_name="${l_mod_name::-2}"
[ "$l_dl" != "y" ] && f_module_fix
else
echo -e " - kernel module: \"$l_mod_name\" doesn't exist in
\"$l_mod_base_directory\""
fi
done
[ "${#a_output3[@]}" -gt 0 ] && printf '%s\n' "" " -- INFO --" " - module:
\"$l_mod_name\" exists in:" "${a_output3[@]}"
[ "${#a_output2[@]}" -gt 0 ] && printf '%s\n' "" "${a_output2[@]}" ||
printf '%s\n' "" " - No changes needed"
printf '%s\n' "" " - remediation of kernel module: \"$l_mod_name\"
complete" ""
}
```

---

### 1.2.1.1 — 1.2.1.1 Ensure GPG keys are configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Most package managers implement GPG key signing to verify package integrity during
installation.

**Rationale**:

It is important to ensure that updates are obtained from a valid source to protect against
spoofing that could lead to the inadvertent installation of malware on the system.

**Audit guide**:

```
Verify GPG keys are configured correctly for your package manager:
# apt-key list
Note:
• apt-key list is deprecated. Manage keyring files in trusted.gpg.d instead
(see apt-key(8)).
• With the deprecation of apt-key it is recommended to use the Signed-By option
in sources.list to require a repository to pass apt-secure(8) verification with a
certain set of keys rather than all trusted keys apt has configured.
- OR -
1. Run the following script and verify GPG keys are configured correctly for your
package manager:
#! /usr/bin/env bash
{
for file in /etc/apt/trusted.gpg.d/*.{gpg,asc}
/etc/apt/sources.list.d/*.{gpg,asc} ; do
if [ -f "$file" ]; then
echo -e "File: $file"
gpg --list-packets "$file" 2>/dev/null | awk '/keyid/ &&
!seen[$NF]++ {print "keyid:", $NF}'
gpg --list-packets "$file" 2>/dev/null | awk '/Signed-By:/ {print
"signed-by:", $NF}'
echo -e
fi
done
}
2. REVIEW and VERIFY to ensure that GPG keys are configured correctly for your
package manager IAW site policy.
```

**Remediation**:

```
Update your package manager GPG keys in accordance with site policy.
```

---

### 1.2.1.2 — 1.2.1.2 Ensure package manager repositories are configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Systems need to have package manager repositories configured to ensure they receive
the latest patches and updates.

**Rationale**:

If a system's package repositories are misconfigured important patches may not be
identified or a rogue repository could introduce compromised software.

**Audit guide**:

```
Run the following command and verify package repositories are configured correctly:
# apt-cache policy
```

**Remediation**:

```
Configure your package manager repositories according to site policy.
```

---

### 1.2.2.1 — 1.2.2.1 Ensure updates, patches, and additional security software (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Periodically patches are released for included software either due to security flaws or to
include additional functionality.

**Rationale**:

Newer patches may contain security enhancements that would not be available through
the latest full update. As a result, it is recommended that the latest software patches be
used to take advantage of the latest functionality. As with any software installation,
organizations need to determine if a given update meets their requirements and verify
the compatibility and supportability of any additional software against the update
revision that is selected.

**Audit guide**:

```
Verify there are no updates or patches to install:
# apt update
# apt -s upgrade
```

**Remediation**:

```
Run the following command to update all packages following local site policy guidance
on applying updates and patches:
# apt update
# apt upgrade
- OR -
# apt dist-upgrade
```

---

### 2.1.22 — 2.1.22 Ensure only approved services are listening on a network (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

A network port is identified by its number, the associated IP address, and the type of the
communication protocol such as TCP or UDP.
A listening port is a network port on which an application or process listens on, acting as
a communication endpoint.
Each listening port can be open or closed (filtered) using a firewall. In general terms, an
open port is a network port that accepts incoming packets from remote locations.

**Rationale**:

Services listening on the system pose a potential risk as an attack vector. These
services should be reviewed, and if not required, the service should be stopped, and the
package containing the service should be removed. If required packages have a
dependency, the service should be stopped and masked to reduce the attack surface of
the system.
Impact:
There may be packages that are dependent on the service's package. If the service's
package is removed, these dependent packages will be removed as well. Before
removing the service's package, review any dependent packages to determine if they
are required on the system.
- IF - a dependent package is required: stop and mask the <service_name>.socket
and <service_name>.service leaving the service's package installed.

**Audit guide**:

```
Run the following command:
# ss -plntu
Review the output to ensure:
• All services listed are required on the system and approved by local site policy.
• Both the port and interface the service is listening on are approved by local site
policy.
• If a listed service is not required:
o Remove the package containing the service
o - IF - the service's package is required for a dependency, stop and mask
the service and/or socket
```

**Remediation**:

```
Run the following commands to stop the service and remove the package containing
the service:
# systemctl stop <service_name>.socket <service_name>.service
# apt purge <package_name>
- OR - If required packages have a dependency:
Run the following commands to stop and mask the service and socket:
# systemctl stop <service_name>.socket <service_name>.service
# systemctl mask <service_name>.socket <service_name>.service
Note: replace <service_name> with the appropriate service name.
```

---

### 3.1.1 — 3.1.1 Ensure IPv6 status is identified (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Internet Protocol Version 6 (IPv6) is the most recent version of Internet Protocol (IP). It's
designed to supply IP addressing and additional security to support the predicted
growth of connected devices. IPv6 is based on 128-bit addressing and can support 340
undecillion, which is 340,282,366,920,938,463,463,374,607,431,768,211,456 unique
addresses.
Features of IPv6
• Hierarchical addressing and routing infrastructure
• Statefull and Stateless configuration
• Support for quality of service (QoS)
• An ideal protocol for neighboring node interaction

**Rationale**:

IETF RFC 4038 recommends that applications are built with an assumption of dual
stack. It is recommended that IPv6 be enabled and configured in accordance with
Benchmark recommendations.
- IF - dual stack and IPv6 are not used in your environment, IPv6 may be disabled to
reduce the attack surface of the system, and recommendations pertaining to IPv6 can
be skipped.
Note: It is recommended that IPv6 be enabled and configured unless this is against
local site policy
Impact:
IETF RFC 4038 recommends that applications are built with an assumption of dual
stack.
When enabled, IPv6 will require additional configuration to reduce risk to the system.

**Audit guide**:

```
Run the following script to identify if IPv6 is enabled on the system:
#!/usr/bin/env bash
{
l_output=""
! grep -Pqs -- '^\h*0\b' /sys/module/ipv6/parameters/disable &&
l_output="- IPv6 is not enabled"
if sysctl net.ipv6.conf.all.disable_ipv6 | grep -Pqs --
"^\h*net\.ipv6\.conf\.all\.disable_ipv6\h*=\h*1\b" && \
sysctl net.ipv6.conf.default.disable_ipv6 | grep -Pqs --
"^\h*net\.ipv6\.conf\.default\.disable_ipv6\h*=\h*1\b"; then
l_output="- IPv6 is not enabled"
fi
[ -z "$l_output" ] && l_output="- IPv6 is enabled"
echo -e "\n$l_output\n"
}
```

**Remediation**:

```
Enable or disable IPv6 in accordance with system requirements and local site policy
Default Value:
IPv6 is enabled
```

---

### 4.2.5 — 4.2.5 Ensure ufw outbound connections are configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Configure the firewall rules for new outbound connections.
Note:
• Changing firewall settings while connected over network can result in being
locked out of the system.
• Unlike iptables, when a new outbound rule is added, ufw automatically takes care
of associated established connections, so no rules for the latter kind are required.

**Rationale**:

If rules are not in place for new outbound connections all packets will be dropped by the
default policy preventing network usage.

**Audit guide**:

```
Run the following command and verify all rules for new outbound connections match
site policy:
# ufw status numbered
```

**Remediation**:

```
Configure ufw in accordance with site policy. The following commands will implement a
policy to allow all outbound connections on all interfaces:
# ufw allow out on all
```

---

### 4.3.3 — 4.3.3 Ensure iptables are flushed with nftables (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

nftables is a replacement for iptables, ip6tables, ebtables and arptables

**Rationale**:

It is possible to mix iptables and nftables. However, this increases complexity and also
the chance to introduce errors. For simplicity flush out all iptables rules, and ensure it is
not loaded

**Audit guide**:

```
Run the following commands to ensure no iptables rules exist
For iptables:
# iptables -L
No rules should be returned
For ip6tables:
# ip6tables -L
No rules should be returned
```

**Remediation**:

```
Run the following commands to flush iptables:
For iptables:
# iptables -F
For ip6tables:
# ip6tables -F
```

---

### 4.3.7 — 4.3.7 Ensure nftables outbound and established connections are (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Configure the firewall rules for new outbound, and established connections

**Rationale**:

If rules are not in place for new outbound, and established connections all packets will
be dropped by the default policy preventing network usage.

**Audit guide**:

```
Run the following commands and verify all rules for established incoming connections
match site policy: site policy:
# nft list ruleset | awk '/hook input/,/}/' | grep -E 'ip protocol (tcp|udp)
ct state'
Output should be similar to:
ip protocol tcp ct state established accept
ip protocol udp ct state established accept
Run the folllowing command and verify all rules for new and established outbound
connections match site policy
# nft list ruleset | awk '/hook output/,/}/' | grep -E 'ip protocol (tcp|udp)
ct state'
Output should be similar to:
ip protocol tcp ct state established,related,new accept
ip protocol udp ct state established,related,new accept
```

**Remediation**:

```
Configure nftables in accordance with site policy. The following commands will
implement a policy to allow all outbound connections and all established connections:
# nft add rule inet filter input ip protocol tcp ct state established accept
# nft add rule inet filter input ip protocol udp ct state established accept
# nft add rule inet filter output ip protocol tcp ct state
new,related,established accept
# nft add rule inet filter output ip protocol udp ct state
new,related,established accept
```

---

### 4.4.2.3 — 4.4.2.3 Ensure iptables outbound and established connections (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Configure the firewall rules for new outbound, and established connections.
Note:
• Changing firewall settings while connected over network can result in being
locked out of the system
• Remediation will only affect the active system firewall, be sure to configure the
default policy in your firewall management to apply on boot as well

**Rationale**:

If rules are not in place for new outbound, and established connections all packets will
be dropped by the default policy preventing network usage.

**Audit guide**:

```
Run the following command and verify all rules for new outbound, and established
connections match site policy:
# iptables -L -v -n
```

**Remediation**:

```
Configure iptables in accordance with site policy. The following commands will
implement a policy to allow all outbound connections and all established connections:
# iptables -A OUTPUT -p tcp -m state --state NEW,ESTABLISHED -j ACCEPT
# iptables -A OUTPUT -p udp -m state --state NEW,ESTABLISHED -j ACCEPT
# iptables -A INPUT -p tcp -m state --state ESTABLISHED -j ACCEPT
# iptables -A INPUT -p udp -m state --state ESTABLISHED -j ACCEPT
```

---

### 4.4.3.3 — 4.4.3.3 Ensure ip6tables outbound and established connections (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Configure the firewall rules for new outbound, and established IPv6 connections.
Note:
• Changing firewall settings while connected over network can result in being
locked out of the system
• Remediation will only affect the active system firewall, be sure to configure the
default policy in your firewall management to apply on boot as well

**Rationale**:

If rules are not in place for new outbound, and established connections all packets will
be dropped by the default policy preventing network usage.

**Audit guide**:

```
Run the following command and verify all rules for new outbound, and established
connections match site policy:
# ip6tables -L -v -n
- OR -
Verify IPv6 is disabled:
Run the following script. Output will confirm if IPv6 is enabled on the system.
#!/usr/bin/env bash
{
l_ipv6_enabled="is"
! grep -Pqs -- '^\h*0\b' /sys/module/ipv6/parameters/disable &&
l_ipv6_enabled="is not"
if sysctl net.ipv6.conf.all.disable_ipv6 | grep -Pqs --
"^\h*net\.ipv6\.conf\.all\.disable_ipv6\h*=\h*1\b" && \
sysctl net.ipv6.conf.default.disable_ipv6 | grep -Pqs --
"^\h*net\.ipv6\.conf\.default\.disable_ipv6\h*=\h*1\b"; then
l_ipv6_enabled="is not"
fi
echo -e " - IPv6 $l_ipv6_enabled enabled on the system"
}
```

**Remediation**:

```
Configure iptables in accordance with site policy. The following commands will
implement a policy to allow all outbound connections and all established connections:
# ip6tables -A OUTPUT -p tcp -m state --state NEW,ESTABLISHED -j ACCEPT
# ip6tables -A OUTPUT -p udp -m state --state NEW,ESTABLISHED -j ACCEPT
# ip6tables -A INPUT -p tcp -m state --state ESTABLISHED -j ACCEPT
# ip6tables -A INPUT -p udp -m state --state ESTABLISHED -j ACCEPT
```

---

### 5.3.3.2.3 — 5.3.3.2.3 Ensure password complexity is configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Password complexity can be set through:
• minclass - The minimum number of classes of characters required in a new
password. (digits, uppercase, lowercase, others). e.g. minclass = 4 requires
digits, uppercase, lower case, and special characters.
• dcredit - The maximum credit for having digits in the new password. If less than
0 it is the minimum number of digits in the new password. e.g. dcredit = -1
requires at least one digit
• ucredit - The maximum credit for having uppercase characters in the new
password. If less than 0 it is the minimum number of uppercase characters in the
new password. e.g. ucredit = -1 requires at least one uppercase character
• ocredit - The maximum credit for having other characters in the new password.
If less than 0 it is the minimum number of other characters in the new password.
e.g. ocredit = -1 requires at least one special character
• lcredit - The maximum credit for having lowercase characters in the new
password. If less than 0 it is the minimum number of lowercase characters in the
new password. e.g. lcredit = -1 requires at least one lowercase character

**Rationale**:

Strong passwords protect systems from being hacked through brute force methods.
Requiring at least one non-alphabetic character increases the search space beyond
pure dictionary words, which makes the resulting password harder to crack.
Forcing users to choose an excessively complex password, e.g. some combination of
upper-case, lower-case, numbers, and special characters, has a negative impact. It
places an extra burden on users and many will use predictable patterns (for example, a
capital letter in the first position, followed by lowercase letters, then one or two numbers,
and a “special character” at the end). Attackers know this, so dictionary attacks will
often contain these common patterns and use the most common substitutions like, $ for
s, @ for a, 1 for l, 0 for o.
Impact:
Passwords that are too complex in nature make it harder for users to remember, leading
to bad practices. In addition, composition requirements provide no defense against
common attack types such as social engineering or insecure storage of passwords

**Audit guide**:

```
Run the following command to verify:
• dcredit, ucredit, lcredit, and ocredit are not set to a value greater than 0
• Complexity conforms to local site policy:
# grep -Psi -- '^\h*(minclass|[dulo]credit)\b' /etc/security/pwquality.conf
/etc/security/pwquality.conf.d/*.conf
Example output:
/etc/security/pwquality.conf.d/50-pwcomplexity.conf:minclass = 3
/etc/security/pwquality.conf.d/50-pwcomplexity.conf:ucredit = -2
/etc/security/pwquality.conf.d/50-pwcomplexity.conf:lcredit = -2
/etc/security/pwquality.conf.d/50-pwcomplexity.conf:dcredit = -1
/etc/security/pwquality.conf.d/50-pwcomplexity.conf:ocredit = 0
The example represents a requirement of three character classes, with passwords
requiring two upper case, two lower case, and one numeric character.
Run the following command to verify that module arguments in the configuration file(s)
are not being overridden by arguments in /etc/pam.d/common-password:
# grep -Psi --
'^\h*password\h+(requisite|required|sufficient)\h+pam_pwquality\.so\h+([^#\n\
r]+\h+)?(minclass=\d*|[dulo]credit=-?\d*)\b' /etc/pam.d/common-password
Nothing should be returned
Note:
• settings should be configured in only one location for clarity
• Settings observe an order of precedence:
o module arguments override the settings in the
/etc/security/pwquality.conf configuration file
o settings in the /etc/security/pwquality.conf configuration file
override settings in a .conf file in the
/etc/security/pwquality.conf.d/ directory
o settings in a .conf file in the /etc/security/pwquality.conf.d/
directory are read in canonical order, with last read file containing the
setting taking precedence
• It is recommended that settings be configured in a .conf file in the
/etc/security/pwquality.conf.d/ directory for clarity, convenience, and
durability.
```

**Remediation**:

```
Run the following command:
# grep -Pl --
'\bpam_pwquality\.so\h+([^#\n\r]+\h+)?(minclass|[dulo]credit)\b'
/usr/share/pam-configs/*
Edit any returned files and remove the minclass, dcredit, ucredit, lcredit, and
ocredit arguments from the pam_pwquality.so line(s)
Create or modify a file ending in .conf in the /etc/security/pwquality.conf.d/
directory or the file /etc/security/pwquality.conf and add or modify the following
line(s) to set complexity according to local site policy:
• minclass = _N_
• dcredit = _N_ # Value should be either 0 or a number proceeded by a minus (-
) symbol
• ucredit = -1 # Value should be either 0 or a number proceeded by a minus (-)
symbol
• ocredit = -1 # Value should be either 0 or a number proceeded by a minus (-)
symbol
• lcredit = -1 # Value should be either 0 or a number proceeded by a minus (-)
symbol
Example 1 - Set minclass = 3:
#!/usr/bin/env bash
{
sed -ri 's/^\s*minclass\s*=/# &/' /etc/security/pwquality.conf
sed -ri 's/^\s*[dulo]credit\s*=/# &/' /etc/security/pwquality.conf
[ ! -d /etc/security/pwquality.conf.d/ ] && mkdir
/etc/security/pwquality.conf.d/
printf '\n%s' "minclass = 3" > /etc/security/pwquality.conf.d/50-
pwcomplexity.conf
}
Example 2 - set dcredit = -1, ucredit = -1, and lcredit = -1:
#!/usr/bin/env bash
{
sed -ri 's/^\s*minclass\s*=/# &/' /etc/security/pwquality.conf
sed -ri 's/^\s*[dulo]credit\s*=/# &/' /etc/security/pwquality.conf
[ ! -d /etc/security/pwquality.conf.d/ ] && mkdir
/etc/security/pwquality.conf.d/
printf '%s\n' "dcredit = -1" "ucredit = -1" "lcredit = -1" >
/etc/security/pwquality.conf.d/50-pwcomplexity.conf
}
Default Value:
minclass = 0
dcredit = 0
ucredit = 0
ocredit = 0
lcredit = 0
```

---

### 5.4.1.2 — 5.4.1.2 Ensure minimum password days is configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

PASS_MIN_DAYS <N> - The minimum number of days allowed between password
changes. Any password changes attempted sooner than this will be rejected. If not
specified, 0 will be assumed (which disables the restriction).

**Rationale**:

Users may have favorite passwords that they like to use because they are easy to
remember, and they believe that their password choice is secure from compromise.
Unfortunately, passwords are compromised and if an attacker is targeting a specific
individual user account, with foreknowledge of data about that user, reuse of old,
potentially compromised passwords, may cause a security breach.
By restricting the frequency of password changes, an administrator can prevent users
from repeatedly changing their password in an attempt to circumvent password reuse
controls
Impact:
If a user’s password is set by other personnel as a procedure in dealing with a lost or
expired password, the user should be forced to update this "set" password with their
own password. e.g. force "change at next logon".
If it is not possible to have a user set their own password immediately, and this
recommendation or local site procedure may cause a user to continue using a third
party generated password, PASS_MIN_DAYS for the effected user should be temporally
changed to 0, to allow a user to change their password immediately.
For applications where the user is not using the password at console, the ability to
"change at next logon" may be limited. This may cause a user to continue to use a
password created by other personnel.

**Audit guide**:

```
Run the following command to verify that PASS_MIN_DAYS is set to a value greater than
0and follows local site policy:
# grep -Pi -- '^\h*PASS_MIN_DAYS\h+\d+\b' /etc/login.defs
Example output:
PASS_MIN_DAYS 1
Run the following command to verify all passwords have a PASS_MIN_DAYS greater than
0:
# awk -F: '($2~/^\$.+\$/) {if($4 < 1)print "User: " $1 " PASS_MIN_DAYS: "
$4}' /etc/shadow
Nothing should be returned
```

**Remediation**:

```
Edit /etc/login.defs and set PASS_MIN_DAYS to a value greater than 0 that follows
local site policy:
Example:
PASS_MIN_DAYS 1
Run the following command to modify user parameters for all users with a password set
to a minimum days greater than zero that follows local site policy:
# chage --mindays <N> <user>
Example:
# awk -F: '($2~/^\$.+\$/) {if($4 < 1)system ("chage --mindays 1 " $1)}'
/etc/shadow
Default Value:
PASS_MIN_DAYS 0
```

---

### 6.1.1.2 — 6.1.1.2 Ensure journald log file access is configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Journald will create logfiles that do not already exist on the system. This setting controls
what permissions will be applied to these newly created files.

**Rationale**:

It is important to ensure that log files have the correct permissions to ensure that
sensitive data is archived and protected.

**Audit guide**:

```
Run the following script to verify:
• systemd-journald logfiles are mode 0640 or more restrictive
• Directories /run/ and /var/lib/systemd/ are mode 0755 or more restrictive
• All other configured directories are mode 2755, 0750, or more restrictive
#!/usr/bin/env bash
{
a_output=() a_output2=()
l_systemd_config_file="/etc/tmpfiles.d/systemd.conf"
l_analyze_cmd="$(readlink -f /bin/systemd-analyze)"
f_file_chk()
{
l_maxperm="$( printf '%o' $(( 0777 & ~$l_perm_mask )) )"
if [ $(( $l_mode & $l_perm_mask )) -le 0 ] || [[ "$l_type" =
"Directory" && "$l_mode" =~ 275(0|5) ]]; then
a_out+=(" - $l_type \"$l_logfile\" access is:" \
" mode: \"$l_mode\", owned by: \"$l_user\", and group owned by:
\"$l_group\"")
else
a_out2+=(" - $l_type \"$l_logfile\" access is:" \
" mode: \"$l_mode\", owned by: \"$l_user\", and group owned by:
\"$l_group\"" \
" should be mode: \"$l_maxperm\" or more restrictive")
fi
}
while IFS= read -r l_file; do
l_file="$(tr -d '# ' <<< "$l_file")" a_out=() a_out2=()
l_logfile_perms_line="$(awk '($1~/^(f|d)$/ && $2~/\/\S+/ && $3~/[0-
9]{3,}/){print $2 ":" $3 ":" $4 ":" $5}' "$l_file")"
while IFS=: read -r l_logfile l_mode l_user l_group; do
if [ -d "$l_logfile" ]; then
l_perm_mask="0027" l_type="Directory"
grep -Psq '^(\/run|\/var\/lib\/systemd)\b' <<< "$l_logfile" &&
l_perm_mask="0022"
else
l_perm_mask="0137" l_type="File"
fi
grep -Psq '^(\/run|\/var\/lib\/systemd)\b' <<< "$l_logfile" &&
l_perm_mask="0022"
f_file_chk
done <<< "$l_logfile_perms_line"
[ "${#a_out[@]}" -gt "0" ] && a_output+=(" - File: \"$l_file\" sets:"
"${a_out[@]}")
[ "${#a_out2[@]}" -gt "0" ] && a_output2+=(" - File: \"$l_file\" sets:"
"${a_out2[@]}")
done < <($l_analyze_cmd cat-config "$l_systemd_config_file" | tac | grep -
Pio '^\h*#\h*\/[^#\n\r\h]+\.conf\b')
if [ "${#a_output2[@]}" -le 0 ]; then
printf '%s\n' "" "- Audit Result:" " ** PASS **" "${a_output[@]}" ""
else
printf '%s\n' "" "- Audit Result:" " ** REVIEW **" \
" - Review file access to ensure they are set IAW site policy:"
"${a_output2[@]}"
[ "${#a_output[@]}" -gt 0 ] && printf '%s\n' "" "- Correctly set:"
"${a_output[@]}" ""
fi
}
Review the output
```

**Remediation**:

```
If the default configuration is not appropriate for the site specific requirements, copy
/usr/lib/tmpfiles.d/systemd.conf to /etc/tmpfiles.d/systemd.conf and
modify as required. Recommended mode for logfiles is 0640 or more restrictive.
```

---

### 6.1.1.3 — 6.1.1.3 Ensure journald log file rotation is configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Journald includes the capability of rotating log files regularly to avoid filling up the
system with logs or making the logs unmanageably large. The file
/etc/systemd/journald.conf is the configuration file used to specify how logs
generated by Journald should be rotated.

**Rationale**:

By keeping the log files smaller and more manageable, a system administrator can
easily archive these files to another system and spend less time looking through
inordinately large log files.

**Audit guide**:

```
Review the systemd-journald configuration. Verify logs are rotated according to site
policy. The specific parameters for log rotation are:
Run the following script and review the output to ensure logs are rotated according to
site policy:
#!/usr/bin/env bash
{
a_output=() a_output2=() l_analyze_cmd="$(readlink -f /bin/systemd-analyze)"
l_systemd_config_file="systemd/journald.conf"
a_parameters=("SystemMaxUse=^.+$" "SystemKeepFree=^.+$" "RuntimeMaxUse=^.+$"
"RuntimeKeepFree=^.+$" "MaxFileSec=^.+$")
f_config_file_parameter_chk()
{
l_used_parameter_setting=""
while IFS= read -r l_file; do
l_file="$(tr -d '# ' <<< "$l_file")"
l_used_parameter_setting="$(grep -PHs -- '^\h*'"$l_parameter_name"'\b'
"$l_file" | tail -n 1)"
[ -n "$l_used_parameter_setting" ] && break
done < <($l_analyze_cmd cat-config "$l_systemd_config_file" | tac | grep -Pio
'^\h*#\h*\/[^#\n\r\h]+\.conf\b')
if [ -n "$l_used_parameter_setting" ]; then
while IFS=: read -r l_file_name l_file_parameter; do
while IFS="=" read -r l_file_parameter_name l_file_parameter_value; do
if grep -Pq -- "$l_parameter_value" <<< "$l_file_parameter_value"; then
a_output+=(" - Parameter: \"${l_file_parameter_name// /}\"" \
" set to: \"${l_file_parameter_value// /}\"" \
" in the file: \"$l_file_name\"")
fi
done <<< "$l_file_parameter"
done <<< "$l_used_parameter_setting"
else
a_output2+=(" - Parameter: \"$l_parameter_name\" is not set in an included
file" \
" *** Note: ***" " \"$l_parameter_name\" May be set in a file that's
ignored by load procedure")
fi
}
for l_input_parameter in "${a_parameters[@]}"; do
while IFS="=" read -r l_parameter_name l_parameter_value; do # Assess and check
parameters
l_parameter_name="${l_parameter_name// /}";
l_parameter_value="${l_parameter_value// /}"
l_value_out="${l_parameter_value//-/ through }";
l_value_out="${l_value_out//|/ or }"
l_value_out="$(tr -d '(){}' <<< "$l_value_out")"
f_config_file_parameter_chk
done <<< "$l_input_parameter"
done
if [ "${#a_output2[@]}" -le 0 ]; then
printf '%s\n' "" "- Audit Result:" " ** PASS **" "${a_output[@]}" ""
else
printf '%s\n' "" "- Audit Result:" " ** FAIL **" " - Reason(s) for audit
failure:" "${a_output2[@]}"
[ "${#a_output[@]}" -gt 0 ] && printf '%s\n' "" "- Correctly set:"
"${a_output[@]}" ""
fi
}
```

**Remediation**:

```
Edit /etc/systemd/journald.conf or a file ending in .conf the
/etc/systemd/journald.conf.d/ directory. Set the following parameters in the
[Journal] section to ensure logs are rotated according to site policy. The settings
should be carefully understood as there are specific edge cases and prioritization of
parameters.
Example Configuration:
[Journal]
SystemMaxUse=1G
SystemKeepFree=500M
RuntimeMaxUse=200M
RuntimeKeepFree=50M
MaxFileSec=1month
Example script to create systemd drop-in configuration file:
{
a_settings=("SystemMaxUse=1G" "SystemKeepFree=500M" "RuntimeMaxUse=200M"
"RuntimeKeepFree=50M" "MaxFileSec=1month")
[ ! -d /etc/systemd/journald.conf.d/ ] && mkdir
/etc/systemd/journald.conf.d/
if grep -Psq -- '^\h*\[Journal\]' /etc/systemd/journald.conf.d/60-
journald.conf; then
printf '%s\n' "" "${a_settings[@]}" >> /etc/systemd/journald.conf.d/60-
journald.conf
else
printf '%s\n' "" "[Journal]" "${a_settings[@]}" >>
/etc/systemd/journald.conf.d/60-journald.conf
fi
}
Note:
• If these settings appear in a canonically later file, or later in the same file, the
setting will be overwritten
• Logfile size and configuration to move logfiles to a remote log server should be
accounted for when configuring these settings
Run to following command to update the parameters in the service:
# systemctl reload-or-restart systemd-journald
```

---

### 6.1.2.1.2 — 6.1.2.1.2 Ensure systemd-journal-upload authentication is (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Journald systemd-journal-upload supports the ability to send log events it gathers to
a remote log host.

**Rationale**:

Storing log data on a remote host protects log integrity from local attacks. If an attacker
gains root access on the local system, they could tamper with or remove log data that is
stored on the local system.
Note: This recommendation only applies if journald is the chosen method for
client side logging. Do not apply this recommendation if rsyslog is used.

**Audit guide**:

```
Run the following script to verify systemd-journal-upload authentication is
configured:
#!/usr/bin/env bash
{
a_output=() a_output2=() l_analyze_cmd="$(readlink -f /bin/systemd-analyze)"
l_systemd_config_file="systemd/journal-upload.conf"
a_parameters=("URL=^.+$" "ServerKeyFile=^.+$" "ServerCertificateFile=^.+$"
"TrustedCertificateFile=^.+$")
f_config_file_parameter_chk()
{
l_used_parameter_setting=""
while IFS= read -r l_file; do
l_file="$(tr -d '# ' <<< "$l_file")"
l_used_parameter_setting="$(grep -PHs -- '^\h*'"$l_parameter_name"'\b'
"$l_file" | tail -n 1)"
[ -n "$l_used_parameter_setting" ] && break
done < <($l_analyze_cmd cat-config "$l_systemd_config_file" | tac | grep -Pio
'^\h*#\h*\/[^#\n\r\h]+\.conf\b')
if [ -n "$l_used_parameter_setting" ]; then
while IFS=: read -r l_file_name l_file_parameter; do
while IFS="=" read -r l_file_parameter_name l_file_parameter_value; do
if grep -Pq -- "$l_parameter_value" <<< "$l_file_parameter_value"; then
a_output+=(" - Parameter: \"${l_file_parameter_name// /}\"" \
" set to: \"${l_file_parameter_value// /}\"" \
" in the file: \"$l_file_name\"")
fi
done <<< "$l_file_parameter"
done <<< "$l_used_parameter_setting"
else
a_output2+=(" - Parameter: \"$l_parameter_name\" is not set in an included
file" \
" *** Note: ***" " \"$l_parameter_name\" May be set in a file that's
ignored by load procedure")
fi
}
for l_input_parameter in "${a_parameters[@]}"; do
while IFS="=" read -r l_parameter_name l_parameter_value; do # Assess and check
parameters
l_parameter_name="${l_parameter_name// /}";
l_parameter_value="${l_parameter_value// /}"
l_value_out="${l_parameter_value//-/ through }";
l_value_out="${l_value_out//|/ or }"
l_value_out="$(tr -d '(){}' <<< "$l_value_out")"
f_config_file_parameter_chk
done <<< "$l_input_parameter"
done
if [ "${#a_output2[@]}" -le 0 ]; then
printf '%s\n' "" "- Audit Result:" " ** PASS **" "${a_output[@]}" ""
else
printf '%s\n' "" "- Audit Result:" " ** FAIL **" " - Reason(s) for audit
failure:" "${a_output2[@]}"
[ "${#a_output[@]}" -gt 0 ] && printf '%s\n' "" "- Correctly set:"
"${a_output[@]}" ""
fi
}
Review the output to ensure it matches your environments' certificate locations and the
URL of the log server:
Example output:
- Audit Result:
** PASS **
- Parameter: "URL"
set to: "192.168.50.42"
in the file: "/etc/systemd/journal-upload.conf.d/60-journald_upload.conf"
- Parameter: "ServerKeyFile"
set to: "/etc/ssl/private/journal-upload.pem"
in the file: "/etc/systemd/journal-upload.conf.d/60-journald_upload.conf"
- Parameter: "ServerCertificateFile"
set to: "/etc/ssl/certs/journal-upload.pem"
in the file: "/etc/systemd/journal-upload.conf.d/60-journald_upload.conf"
- Parameter: "TrustedCertificateFile"
set to: "/etc/ssl/ca/trusted.pem"
in the file: "/etc/systemd/journal-upload.conf.d/60-journald_upload.conf"
```

**Remediation**:

```
Edit the /etc/systemd/journal-upload.conf file or a file in
/etc/systemd/journal-upload.conf.d ending in .conf and ensure the following
lines are set in the [Upload] section per your environment:
Example settings:
[Upload]
URL=192.168.50.42
ServerKeyFile=/etc/ssl/private/journal-upload.pem
ServerCertificateFile=/etc/ssl/certs/journal-upload.pem
TrustedCertificateFile=/etc/ssl/ca/trusted.pem
Example script to create systemd drop-in configuration file:
#!/usr/bin/env bash
{
a_settings=("URL=192.168.50.42" "ServerKeyFile=/etc/ssl/private/journal-
upload.pem" \
"ServerCertificateFile=/etc/ssl/certs/journal-upload.pem"
"TrustedCertificateFile=/etc/ssl/ca/trusted.pem")
[ ! -d /etc/systemd/journal-upload.conf.d/ ] && mkdir
/etc/systemd/journal-upload.conf.d/
if grep -Psq -- '^\h*\[Upload\]' /etc/systemd/journal-upload.conf.d/60-
journald_upload.conf; then
printf '%s\n' "" "${a_settings[@]}" >> /etc/systemd/journal-
upload.conf.d/60-journald_upload.conf
else
printf '%s\n' "" "[Journal]" "${a_settings[@]}" >>
/etc/systemd/journal-upload.conf.d/60-journald_upload.conf
fi
}
Run the following command to update the parameters in the service:
# systemctl reload-or-restart systemd-journal-upload
```

---

### 6.1.3.5 — 6.1.3.5 Ensure rsyslog logging is configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

The rsyslog and configuration files specifies rules for logging and which files are to be
used to log certain classes of messages.

**Rationale**:

A great deal of important security-related information is sent via rsyslog (e.g.,
successful and failed su attempts, failed login attempts, root login attempts, etc.).

**Audit guide**:

```
Review the contents of /etc/rsyslog.conf and /etc/rsyslog.d/*.conf files to
ensure appropriate logging is set. In addition, run the following command and verify that
the log files are logging information as expected:
Run the following script and review the output from the rsyslog configuration to ensure
appropriate logging is set an in accordance with local site policy.
#!/usr/bin/env bash
{
l_analyze_cmd="$(readlink -f /bin/systemd-analyze)"
l_include='\$IncludeConfig' a_config_files=("rsyslog.conf")
while IFS= read -r l_file; do
l_conf_loc="$(awk '$1~/^\s*'"$l_include"'$/ {print $2}' "$(tr -d '# '
<<< "$l_file")" | tail -n 1)"
[ -n "$l_conf_loc" ] && break
done < <($l_analyze_cmd cat-config "${a_config_files[@]}" | tac | grep -
Pio '^\h*#\h*\/[^#\n\r\h]+\.conf\b')
if [ -d "$l_conf_loc" ]; then
l_dir="$l_conf_loc" l_ext="*"
elif grep -Psq '\/\*\.([^#/\n\r]+)?\h*$' <<< "$l_conf_loc" || [ -f
"$(readlink -f "$l_conf_loc")" ]; then
l_dir="$(dirname "$l_conf_loc")" l_ext="$(basename "$l_conf_loc")"
fi
while read -r -d $'\0' l_file_name; do
[ -f "$(readlink -f "$l_file_name")" ] && a_config_files+=("$(readlink
-f "$l_file_name")")
done < <(find -L "$l_dir" -type f -name "$l_ext" -print0 2>/dev/null)
for l_logfile in "${a_config_files[@]}"; do
grep -PHs -- '^\h*[^#\n\r\/:]+\/var\/log\/.*$' "$l_logfile"
done
}
Example output:
/etc/rsyslog.d/60-rsyslog.conf:auth,authpriv.* /var/log/secure
/etc/rsyslog.d/60-rsyslog.conf:mail.* -/var/log/mail
/etc/rsyslog.d/60-rsyslog.conf:mail.info -/var/log/mail.info
/etc/rsyslog.d/60-rsyslog.conf:mail.warning -/var/log/mail.warn
/etc/rsyslog.d/60-rsyslog.conf:mail.err /var/log/mail.err
/etc/rsyslog.d/60-rsyslog.conf:cron.* /var/log/cron
/etc/rsyslog.d/60-rsyslog.conf:*.=warning;*.=err -/var/log/warn
/etc/rsyslog.d/60-rsyslog.conf:*.crit /var/log/warn
/etc/rsyslog.d/60-rsyslog.conf:*.*;mail.none;news.none -/var/log/messages
/etc/rsyslog.d/60-rsyslog.conf:local0,local1.* -
/var/log/localmessages
/etc/rsyslog.d/60-rsyslog.conf:local2,local3.* -
/var/log/localmessages
/etc/rsyslog.d/60-rsyslog.conf:local4,local5.* -
/var/log/localmessages
/etc/rsyslog.d/60-rsyslog.conf:local6,local7.* -
/var/log/localmessages
/etc/rsyslog.d/50-default.conf:auth,authpriv.* /var/log/auth.log
#<- Will be ignored
/etc/rsyslog.d/50-default.conf:*.*;auth,authpriv.none -/var/log/syslog
/etc/rsyslog.d/50-default.conf:kern.* -/var/log/kern.log
/etc/rsyslog.d/50-default.conf:mail.* -/var/log/mail.log
#<- Will be ignored
/etc/rsyslog.d/50-default.conf:mail.err /var/log/mail.err
#<- Will be ignored
Note:
• Output is generated as <CONFIGURATION_FILE>:<PARAMETER>
• Files are listed in order of precedence. If the same parameter is listed multiple
times, only the first occurrence will be used be the rsyslog daemon
```

**Remediation**:

```
Edit the following lines in the configuration file(s) returned by the audit as appropriate for
your environment.
Note: The below configuration is shown for example purposes only. Due care should be
given to how the organization wishes to store log data.
*.emerg :omusrmsg:*
auth,authpriv.* /var/log/secure
mail.* -/var/log/mail
mail.info -/var/log/mail.info
mail.warning -/var/log/mail.warn
mail.err /var/log/mail.err
cron.* /var/log/cron
*.=warning;*.=err -/var/log/warn
*.crit /var/log/warn
*.*;mail.none;news.none -/var/log/messages
local0,local1.* -/var/log/localmessages
local2,local3.* -/var/log/localmessages
local4,local5.* -/var/log/localmessages
local6,local7.* -/var/log/localmessages
Run the following command to reload the rsyslogd configuration:
# systemctl reload-or-restart rsyslog
```

---

### 6.1.3.6 — 6.1.3.6 Ensure rsyslog is configured to send logs to a remote log (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

rsyslog supports the ability to send log events it gathers to a remote log host or to
receive messages from remote hosts, thus enabling centralized log management.

**Rationale**:

Storing log data on a remote host protects log integrity from local attacks. If an attacker
gains root access on the local system, they could tamper with or remove log data that is
stored on the local system.

**Audit guide**:

```
Run the following script and review the output of rsyslog configuration. Verify that logs
are sent to a central host used by your organization:
basic format
#!/usr/bin/env bash
{
l_analyze_cmd="$(readlink -f /bin/systemd-analyze)"
l_include='\$IncludeConfig' a_config_files=("rsyslog.conf")
while IFS= read -r l_file; do
l_conf_loc="$(awk '$1~/^\s*'"$l_include"'$/ {print $2}' "$(tr -d '# '
<<< "$l_file")" | tail -n 1)"
[ -n "$l_conf_loc" ] && break
done < <($l_analyze_cmd cat-config "${a_config_files[@]}" | tac | grep -
Pio '^\h*#\h*\/[^#\n\r\h]+\.conf\b')
if [ -d "$l_conf_loc" ]; then
l_dir="$l_conf_loc" l_ext="*"
elif grep -Psq '\/\*\.([^#/\n\r]+)?\h*$' <<< "$l_conf_loc" || [ -f
"$(readlink -f "$l_conf_loc")" ]; then
l_dir="$(dirname "$l_conf_loc")" l_ext="$(basename "$l_conf_loc")"
fi
while read -r -d $'\0' l_file_name; do
[ -f "$(readlink -f "$l_file_name")" ] && a_config_files+=("$(readlink
-f "$l_file_name")")
done < <(find -L "$l_dir" -type f -name "$l_ext" -print0 2>/dev/null)
for l_logfile in "${a_config_files[@]}"; do
grep -Hs -- "^*.*[^I][^I]*@" "$l_logfile"
done
}
Output should include @@<FQDN or IP of remote loghost>:
Example output:
/etc/rsyslog.d/60-rsyslog.conf:*.* @@loghost.example.com
- OR -
Run the following script and review the output of rsyslog configuration. Verify that logs
are sent to a central host used by your organization:
advanced format
#!/usr/bin/env bash
{
l_analyze_cmd="$(readlink -f /bin/systemd-analyze)"
l_include='\$IncludeConfig' a_config_files=("rsyslog.conf")
while IFS= read -r l_file; do
l_conf_loc="$(awk '$1~/^\s*'"$l_include"'$/ {print $2}' "$(tr -d '# '
<<< "$l_file")" | tail -n 1)"
[ -n "$l_conf_loc" ] && break
done < <($l_analyze_cmd cat-config "${a_config_files[@]}" | tac | grep -
Pio '^\h*#\h*\/[^#\n\r\h]+\.conf\b')
if [ -d "$l_conf_loc" ]; then
l_dir="$l_conf_loc" l_ext="*"
elif grep -Psq '\/\*\.([^#/\n\r]+)?\h*$' <<< "$l_conf_loc" || [ -f
"$(readlink -f "$l_conf_loc")" ]; then
l_dir="$(dirname "$l_conf_loc")" l_ext="$(basename "$l_conf_loc")"
fi
while read -r -d $'\0' l_file_name; do
[ -f "$(readlink -f "$l_file_name")" ] && a_config_files+=("$(readlink
-f "$l_file_name")")
done < <(find -L "$l_dir" -type f -name "$l_ext" -print0 2>/dev/null)
for l_logfile in "${a_config_files[@]}"; do
grep -PHsi --
'^\s*([^#]+\s+)?action\(([^#]+\s+)?\btarget=\"?[^#"]+\"?\b' "$l_logfile"
done
}
Output should include target=<FQDN or IP of remote loghost>:
Example output:
/etc/rsyslog.d/60-rsyslog.conf:*.* action(type="omfwd"
target="loghost.example.com" port="514" protocol="tcp"
```

**Remediation**:

```
Edit the rsyslog configuration and add the following line (where
loghost.example.com is the name of your central log host). The target directive may
either be a fully qualified domain name or an IP address.
Example script to create a drop-in configuration file:
#!/usr/bin/env bash
{
a_parameters=('*.* action(type="omfwd" target="loghost.example.com"
port="514" protocol="tcp"' \
' action.resumeRetryCount="100"' '
queue.type="LinkedList" queue.size="1000")')
[ ! -d "/etc/rsyslog.d/" ] && mkdir /etc/rsyslod.d/
printf '%s\n' "" "${a_parameters[@]}" >> /etc/rsyslog.d/60-rsyslog.conf
}
Run the following command to reload rsyslog.service:
# systemctl reload-or-restart rsyslog.service
```

---

### 6.1.3.8 — 6.1.3.8 Ensure logrotate is configured (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

The system includes the capability of rotating log files regularly to avoid filling up the
system with logs or making the logs unmanageably large. The file
/etc/logrotate.d/rsyslog is the configuration file used to rotate log files created by
rsyslog.

**Rationale**:

By keeping the log files smaller and more manageable, a system administrator can
easily archive these files to another system and spend less time looking through
inordinately large log files.

**Audit guide**:

```
Run the following script to analyze the logrotate configuration:
#!/usr/bin/env bash
{
l_analyze_cmd="$(readlink -f /bin/systemd-analyze)"
l_config_file="/etc/logrotate.conf"
l_include="$(awk '$1~/^\s*include$/{print$2}' "$l_config_file"
2>/dev/null)"
[ -d "$l_include" ] && l_include="$l_include/*"
$l_analyze_cmd cat-config "$l_config_file" $l_include
}
Note: The last occurrence of a argument is the one used for the logrotate
configuration
```

**Remediation**:

```
Edit /etc/logrotate.conf, or the appropriate configuration file provided by the script
in the Audit Procedure, as necessary to ensure logs are rotated according to site policy.
```

---

### 6.2.3.21 — 6.2.3.21 Ensure the running and on disk configuration is the same (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

The Audit system have both on disk and running configuration. It is possible for these
configuration settings to differ.
Note: Due to the limitations of augenrules and auditctl, it is not absolutely
guaranteed that loading the rule sets via augenrules --load will result in all rules
being loaded or even that the user will be informed if there was a problem loading the
rules.

**Rationale**:

Configuration differences between what is currently running and what is on disk could
cause unexpected problems or may give a false impression of compliance
requirements.

**Audit guide**:

```
Merged rule sets
Ensure that all rules in /etc/audit/rules.d have been merged into
/etc/audit/audit.rules:
# augenrules --check
/usr/sbin/augenrules: No change
Should there be any drift, run augenrules --load to merge and load all rules.
```

**Remediation**:

```
If the rules are not aligned across all three () areas, run the following command to
merge and load all rules:
# augenrules --load
Check if reboot is required.
if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then echo "Reboot required
to load rules"; fi
```

---

### 7.1.13 — 7.1.13 Ensure SUID and SGID files are reviewed (Manual)

**Reason**: `assessment_status=Manual (manual review required)`

**Assessment**: Manual

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

The owner of a file can set the file's permissions to run with the owner's or group's
permissions, even if the user running the program is not the owner or a member of the
group. The most common reason for a SUID or SGID program is to enable users to
perform functions (such as changing their password) that require root privileges.

**Rationale**:

There are valid reasons for SUID and SGID programs, but it is important to identify and
review such programs to ensure they are legitimate. Review the files returned by the
action in the audit section and check to see if system binaries have a different
checksum than what from the package. This is an indication that the binary may have
been replaced.

**Audit guide**:

```
Run the following script to generate a list of SUID and SGID files:
#!/usr/bin/env bash
{
l_output="" l_output2=""
a_suid=(); a_sgid=() # initialize arrays
while IFS= read -r l_mount; do
while IFS= read -r -d $'\0' l_file; do
if [ -e "$l_file" ]; then
l_mode="$(stat -Lc '%#a' "$l_file")"
[ $(( $l_mode & 04000 )) -gt 0 ] && a_suid+=("$l_file")
[ $(( $l_mode & 02000 )) -gt 0 ] && a_sgid+=("$l_file")
fi
done < <(find "$l_mount" -xdev -type f \( -perm -2000 -o -perm -4000 \)
-print0 2>/dev/null)
done < <(findmnt -Dkerno fstype,target,options | awk '($1 !~
/^\s*(nfs|proc|smb|vfat|iso9660|efivarfs|selinuxfs)/ && $2 !~
/^\/run\/user\// && $3 !~/noexec/ && $3 !~/nosuid/) {print $2}')
if ! (( ${#a_suid[@]} > 0 )); then
l_output="$l_output\n - No executable SUID files exist on the system"
else
l_output2="$l_output2\n - List of \"$(printf '%s' "${#a_suid[@]}")\"
SUID executable files:\n$(printf '%s\n' "${a_suid[@]}")\n - end of list -\n"
fi
if ! (( ${#a_sgid[@]} > 0 )); then
l_output="$l_output\n - No SGID files exist on the system"
else
l_output2="$l_output2\n - List of \"$(printf '%s' "${#a_sgid[@]}")\"
SGID executable files:\n$(printf '%s\n' "${a_sgid[@]}")\n - end of list -\n"
fi
[ -n "$l_output2" ] && l_output2="$l_output2\n- Review the preceding
list(s) of SUID and/or SGID files to\n- ensure that no rogue programs have
been introduced onto the system.\n"
unset a_arr; unset a_suid; unset a_sgid # Remove arrays
# If l_output2 is empty, Nothing to report
if [ -z "$l_output2" ]; then
echo -e "\n- Audit Result:\n$l_output\n"
else
echo -e "\n- Audit Result:\n$l_output2\n"
[ -n "$l_output" ] && echo -e "$l_output\n"
fi
}
Note: on systems with a large number of files, this may be a long running process
```

**Remediation**:

```
Ensure that no rogue SUID or SGID programs have been introduced into the system.
Review the files returned by the action in the Audit section and confirm the integrity of
these binaries.
```

---

## NoMarker (자동 변환 패턴 미매칭, 49건)

audit text가 9 자동 변환 패턴(PASS marker / Nothing returned / is installed / stat permission / sshd boolean·numeric·range / multi-line cmd / hashbang body wrap / grep verify / awk exact)에 잡히지 않은 항목들. 향후 converter 패턴 확장 또는 수동 fixture 작성으로 cover 가능.

### 1.3.1.3 — 1.3.1.3 Ensure all AppArmor Profiles are in enforce or complain (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

AppArmor profiles define what resources applications are able to access.

**Rationale**:

Security configuration requirements vary from site to site. Some sites may mandate a
policy that is stricter than the default policy, which is perfectly acceptable. This item is
intended to ensure that any policies that exist on the system are activated.

**Audit guide**:

```
Run the following command and verify that profiles are loaded, and are in either enforce
or complain mode:
# apparmor_status | grep profiles
Review output and ensure that profiles are loaded, and in either enforce or complain
mode:
37 profiles are loaded.
35 profiles are in enforce mode.
2 profiles are in complain mode.
4 processes have profiles defined.
Run the following command and verify no processes are unconfined
# apparmor_status | grep processes
Review the output and ensure no processes are unconfined:
4 processes have profiles defined.
4 processes are in enforce mode.
0 processes are in complain mode.
0 processes are unconfined but have a profile defined.
```

**Remediation**:

```
Run the following command to set all profiles to enforce mode:
# aa-enforce /etc/apparmor.d/*
- OR -
Run the following command to set all profiles to complain mode:
# aa-complain /etc/apparmor.d/*
Note: Any unconfined processes may need to have a profile created or activated for
them and then be restarted.
```

---

### 1.3.1.4 — 1.3.1.4 Ensure all AppArmor Profiles are enforcing (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

AppArmor profiles define what resources applications are able to access.

**Rationale**:

Security configuration requirements vary from site to site. Some sites may mandate a
policy that is stricter than the default policy, which is perfectly acceptable. This item is
intended to ensure that any policies that exist on the system are activated.

**Audit guide**:

```
Run the following commands and verify that profiles are loaded and are not in complain
mode:
# apparmor_status | grep profiles
Review output and ensure that profiles are loaded, and in enforce mode:
34 profiles are loaded.
34 profiles are in enforce mode.
0 profiles are in complain mode.
2 processes have profiles defined.
Run the following command and verify that no processes are unconfined:
apparmor_status | grep processes
Review the output and ensure no processes are unconfined:
2 processes have profiles defined.
2 processes are in enforce mode.
0 processes are in complain mode.
0 processes are unconfined but have a profile defined.
```

**Remediation**:

```
Run the following command to set all profiles to enforce mode:
# aa-enforce /etc/apparmor.d/*
Note: Any unconfined processes may need to have a profile created or activated for
them and then be restarted
```

---

### 1.4.1 — 1.4.1 Ensure bootloader password is set (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Setting the boot loader password will require that anyone rebooting the system must
enter a password before being able to set command line boot parameters

**Rationale**:

Requiring a boot password upon execution of the boot loader will prevent an
unauthorized user from entering boot parameters or changing the boot partition. This
prevents users from weakening security (e.g. turning off AppArmor at boot time).
Impact:
If password protection is enabled, only the designated superuser can edit a GRUB 2
menu item by pressing "e" or access the GRUB 2 command line by pressing "c"
If GRUB 2 is set up to boot automatically to a password-protected menu entry the user
has no option to back out of the password prompt to select another menu entry. Holding
the SHIFT key will not display the menu in this case. The user must enter the correct
username and password. If unable to do so, the configuration files will have to be edited
via a LiveCD or other means to fix the problem
You can add --unrestricted to the menu entries to allow the system to boot without
entering a password. A password will still be required to edit menu items.
More Information: https://help.ubuntu.com/community/Grub2/Passwords

**Audit guide**:

```
Run the following commands and verify output matches:
# grep "^set superusers" /boot/grub/grub.cfg
set superusers="<username>"
# awk -F. '/^\s*password/ {print $1"."$2"."$3}' /boot/grub/grub.cfg
password_pbkdf2 <username> grub.pbkdf2.sha512
```

**Remediation**:

```
Create an encrypted password with grub-mkpasswd-pbkdf2:
# grub-mkpasswd-pbkdf2 --iteration-count=600000 --salt=64
Enter password: <password>
Reenter password: <password>
PBKDF2 hash of your password is <encrypted-password>
Add the following into a custom /etc/grub.d configuration file:
cat <<EOF
exec tail -n +2 $0
set superusers="<username>"
password_pbkdf2 <username> <encrypted-password>
EOF
The superuser/user information and password should not be contained in the
/etc/grub.d/00_header file as this file could be overwritten in a package update.
If there is a requirement to be able to boot/reboot without entering the password, edit
/etc/grub.d/10_linux and add --unrestricted to the line CLASS=
Example:
CLASS="--class gnu-linux --class gnu --class os --unrestricted"
Run the following command to update the grub2 configuration:
# update-grub
Default Value:
This recommendation is designed around the grub bootloader, if LILO or another
bootloader is in use in your environment enact equivalent settings.
Replace /boot/grub/grub.cfg with the appropriate grub configuration file for your
environment.
```

---

### 1.6.4 — 1.6.4 Ensure access to /etc/motd is configured (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

The contents of the /etc/motd file are displayed to users after login and function as a
message of the day for authenticated users.

**Rationale**:

- IF - the /etc/motd file does not have the correct access configured, it could be
modified by unauthorized users with incorrect or misleading information.

**Audit guide**:

```
Run the following command and verify that if /etc/motd exists, Access is 644 or more
restrictive, Uid and Gid are both 0/root:
# [ -e /etc/motd ] && stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: { %g/
%G)' /etc/motd
Access: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)
-- OR --
Nothing is returned
```

**Remediation**:

```
Run the following commands to set mode, owner, and group on /etc/motd:
# chown root:root $(readlink -e /etc/motd)
# chmod u-x,go-wx $(readlink -e /etc/motd)
- OR -
Run the following command to remove the /etc/motd file:
# rm /etc/motd
```

---

### 1.7.1 — 1.7.1 Ensure GDM is removed (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server

**Description**:

The GNOME Display Manager (GDM) is a program that manages graphical display
servers and handles graphical user logins.

**Rationale**:

If a Graphical User Interface (GUI) is not required, it should be removed to reduce the
attack surface of the system.
Impact:
Removing the GNOME Display manager will remove the Graphical User Interface (GUI)
from the system.

**Audit guide**:

```
Run the following command and verify gdm3 is not installed:
# dpkg-query -W -f='${binary:Package}\t${Status}\t${db:Status-Status}\n' gdm3
gdm3 unknown ok not-installed not-installed
```

**Remediation**:

```
Run the following commands to uninstall gdm3 and remove unused dependencies:
# apt purge gdm3
# apt autoremove gdm3
```

---

### 1.7.4 — 1.7.4 Ensure GDM screen locks when the user is idle (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

GNOME Desktop Manager can make the screen lock automatically whenever the user
is idle for some amount of time.

**Rationale**:

Setting a lock-out value reduces the window of opportunity for unauthorized user access
to another user's session that has been left unattended.

**Audit guide**:

```
Run the following commands to verify that the screen locks when the user is idle:
# gsettings get org.gnome.desktop.screensaver lock-delay
uint32 5
# gsettings get org.gnome.desktop.session idle-delay
uint32 900
Notes:
• lock-delay=uint32 {n} - should be 5 seconds or less and follow local site
policy
• idle-delay=uint32 {n} - Should be 900 seconds (15 minutes) or less, not 0
(disabled) and follow local site policy
```

**Remediation**:

```
- IF - A user profile is already created run the following commands to enable screen
locks when the user is idle:
# gsettings set org.gnome.desktop.screensaver lock-delay 5
# gsettings set org.gnome.desktop.session idle-delay 900
Note:
• gsettings commands in this section MUST be done from a command window
on a graphical desktop or an error will be returned.
• The system must be restarted after all gsettings configurations have been set
in order for CIS-CAT Assessor to appropriately assess.
- OR/IF- A user profile does not exist:
1. Create or edit the user profile in the /etc/dconf/profile/ and verify it includes
the following:
user-db:user
system-db:{NAME_OF_DCONF_DATABASE}
Note: local is the name of a dconf database used in the examples.
2. Create the directory /etc/dconf/db/local.d/ if it doesn't already exist:
3. Create the key file /etc/dconf/db/local.d/00-screensaver to provide
information for the local database:
Example key file:
# Specify the dconf path
[org/gnome/desktop/session]
# Number of seconds of inactivity before the screen goes blank
# Set to 0 seconds if you want to deactivate the screensaver.
idle-delay=uint32 180
# Specify the dconf path
[org/gnome/desktop/screensaver]
# Number of seconds after the screen is blank before locking the screen
lock-delay=uint32 0
Note: You must include the uint32 along with the integer key values as shown.
4. Run the following command to update the system databases:
# dconf update
5. Users must log out and back in again before the system-wide settings take effect.
```

---

### 1.7.6 — 1.7.6 Ensure GDM automatic mounting of removable media is (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 2 - Workstation

**Description**:

By default GNOME automatically mounts removable media when inserted as a
convenience to the user.

**Rationale**:

With automounting enabled anyone with physical access could attach a USB drive or
disc and have its contents available in system even if they lacked permissions to mount
it themselves.
Impact:
The use of portable hard drives is very common for workstation users. If your
organization allows the use of portable storage or media on workstations and physical
access controls to workstations is considered adequate there is little value add in
turning off automounting.

**Audit guide**:

```
Run the following commands to verify automatic mounting is disabled:
# gsettings get org.gnome.desktop.media-handling automount
false
# gsettings get org.gnome.desktop.media-handling automount-open
false
```

**Remediation**:

```
- IF - A user profile exists run the following commands to ensure automatic mounting is
disabled:
# gsettings set org.gnome.desktop.media-handling automount false
# gsettings set org.gnome.desktop.media-handling automount-open false
Note:
• gsettings commands in this section MUST be done from a command window
on a graphical desktop or an error will be returned.
• The system must be restarted after all gsettings configurations have been set
in order for CIS-CAT Assessor to appropriately assess.
- OR/IF - A user profile does not exist:
1. Create a file /etc/dconf/db/local.d/00-media-automount with following
content:
[org/gnome/desktop/media-handling]
automount=false
automount-open=false
2. After creating the file, apply the changes using below command :
# dconf update
Note: Users must log out and back in again before the system-wide settings take effect.
```

---

### 1.7.8 — 1.7.8 Ensure GDM autorun-never is enabled (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

The autorun-never setting allows the GNOME Desktop Display Manager to disable
autorun through GDM.

**Rationale**:

Malware on removable media may taking advantage of Autorun features when the
media is inserted into a system and execute.

**Audit guide**:

```
Run the following command to verify that autorun-never is set to true for GDM:
# gsettings get org.gnome.desktop.media-handling autorun-never
true
```

**Remediation**:

```
- IF - A user profile exists run the following command to set autorun-never to true for
GDM users:
# gsettings set org.gnome.desktop.media-handling autorun-never true
Note:
• gsettings commands in this section MUST be done from a command window
on a graphical desktop or an error will be returned.
• The system must be restarted after all gsettings configurations have been set
in order for CIS-CAT Assessor to appropriately assess.
- OR/IF - A user profile does not exist:
1. create the file /etc/dconf/db/local.d/locks/00-media-autorun with the
following content:
[org/gnome/desktop/media-handling]
autorun-never=true
2. Update the systems databases:
# dconf update
Note: Users must log out and back in again before the system-wide settings take effect.
Default Value:
false
```

---

### 2.1.20 — 2.1.20 Ensure X window server services are not in use (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server

**Description**:

The X Window System provides a Graphical User Interface (GUI) where users can have
multiple windows in which to run programs and various add on. The X Windows system
is typically used on workstations where users login, but not on servers where users
typically do not login.

**Rationale**:

Unless your organization specifically requires graphical login access via X Windows,
remove it to reduce the potential attack surface.
Impact:
If a Graphical Desktop Manager (GDM) is in use on the system, there may be a
dependency on the xorg-x11-server-common package. If the GDM is required and
approved by local site policy, the package should not be removed.
Many Linux systems run applications which require a Java runtime. Some Linux Java
packages have a dependency on specific X Windows xorg-x11-fonts. One workaround
to avoid this dependency is to use the "headless" Java packages for your specific Java
runtime.

**Audit guide**:

```
- IF - a Graphical Desktop Manager or X-Windows server is not required and approved
by local site policy:
Run the following command to Verify X Windows Server is not installed.
dpkg-query -s xserver-common &>/dev/null && echo "xserver-common is
installed"
Nothing should be returned
```

**Remediation**:

```
- IF - a Graphical Desktop Manager or X-Windows server is not required and approved
by local site policy:
Run the following command to remove the X Windows Server package:
# apt purge xserver-common
```

---

### 4.2.4 — 4.2.4 Ensure ufw loopback traffic is configured (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Configure the loopback interface to accept traffic. Configure all other interfaces to deny
traffic to the loopback network (127.0.0.0/8 for IPv4 and ::1/128 for IPv6).

**Rationale**:

Loopback traffic is generated between processes on machine and is typically critical to
operation of the system. The loopback interface is the only place that loopback network
(127.0.0.0/8 for IPv4 and ::1/128 for IPv6) traffic should be seen, all other interfaces
should ignore traffic on this network as an anti-spoofing measure.

**Audit guide**:

```
Run the following command and verify loopback interface to accept traffic:
# grep -P -- 'lo|127.0.0.0' /etc/ufw/before.rules
Output includes:
# allow all on loopback
-A ufw-before-input -i lo -j ACCEPT
-A ufw-before-output -o lo -j ACCEPT
Run the following command and verify all other interfaces deny traffic to the loopback
network (127.0.0.0/8 for IPv4 and ::1/128 for IPv6)
# ufw status verbose
To Action From
-- ------ ----
Anywhere DENY IN 127.0.0.0/8
Anywhere (v6) DENY IN ::1
Note: ufw status only shows rules added with ufw and not the rules found in the
/etc/ufw rules files where allow all on loopback is configured by default.
```

**Remediation**:

```
Run the following commands to configure the loopback interface to accept traffic:
# ufw allow in on lo
# ufw allow out on lo
Run the following commands to configure all other interfaces to deny traffic to the
loopback network:
# ufw deny in from 127.0.0.0/8
# ufw deny in from ::1
Default Value:
# allow all on loopback
-A ufw-before-input -i lo -j ACCEPT
-A ufw-before-output -o lo -j ACCEPT
```

---

### 4.2.6 — 4.2.6 Ensure ufw firewall rules exist for all open ports (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Services and ports can be accepted or explicitly rejected.
Note:
• Changing firewall settings while connected over network can result in being
locked out of the system
• The remediation command opens up the port to traffic from all sources. Consult
ufw documentation and set any restrictions in compliance with site policy

**Rationale**:

To reduce the attack surface of a system, all services and ports should be blocked
unless required.
• Any ports that have been opened on non-loopback addresses need firewall rules
to govern traffic.
• Without a firewall rule configured for open ports, the default firewall policy will
drop all packets to these ports.
• Required ports should have a firewall rule created to allow approved connections
in accordance with local site policy.
• Unapproved ports should have an explicit deny rule created.

**Audit guide**:

```
Run the following script to verify a firewall rule exists for all open ports:
#!/usr/bin/env bash
{
unset a_ufwout;unset a_openports
while read -r l_ufwport; do
[ -n "$l_ufwport" ] && a_ufwout+=("$l_ufwport")
done < <(ufw status verbose | grep -Po '^\h*\d+\b' | sort -u)
while read -r l_openport; do
[ -n "$l_openport" ] && a_openports+=("$l_openport")
done < <(ss -tuln | awk '($5!~/%lo:/ && $5!~/127.0.0.1:/ &&
$5!~/\[?::1\]?:/) {split($5, a, ":"); print a[2]}' | sort -u)
a_diff=("$(printf '%s\n' "${a_openports[@]}" "${a_ufwout[@]}"
"${a_ufwout[@]}" | sort | uniq -u)")
if [[ -n "${a_diff[*]}" ]]; then
echo -e "\n- Audit Result:\n ** FAIL **\n- The following port(s) don't
have a rule in UFW: $(printf '%s\n' \\n"${a_diff[*]}")\n- End List"
else
echo -e "\n - Audit Passed -\n- All open ports have a rule in UFW\n"
fi
}
```

**Remediation**:

```
For each port identified in the audit which does not have a firewall rule, evaluate the
service listening on the port and add a rule for accepting or denying inbound
connections in accordance with local site policy:
Examples:
# ufw allow in <port>/<tcp or udp protocol>
# ufw deny in <port>/<tcp or udp protocol>
Note: Examples create rules for from any, to any. More specific rules should be
concentered when allowing inbound traffic e.g only traffic from this network.
Example to allow traffic on port 443 using the tcp protocol from the 192.168.1.0 network:
ufw allow from 192.168.1.0/24 to any proto tcp port 443
```

---

### 4.2.7 — 4.2.7 Ensure ufw default deny firewall policy (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

A default deny policy on connections ensures that any unconfigured network usage will
be rejected.
Note: Any port or protocol without a explicit allow before the default deny will be
blocked

**Rationale**:

With a default accept policy the firewall will accept any packet that is not configured to
be denied. It is easier to allow list acceptable usage than to deny list unacceptable
usage.
Impact:
Any port and protocol not explicitly allowed will be blocked. The following rules should
be considered before applying the default deny.
ufw allow out http
ufw allow out https
ufw allow out ntp # Network Time Protocol
ufw allow out to any port 53 # DNS
ufw allow out to any port 853 # DNS over TLS
ufw logging on

**Audit guide**:

```
Run the following command and verify that the default policy for incoming , outgoing ,
and routed directions is deny , reject , or disabled:
# ufw status verbose | grep Default:
Example output:
Default: deny (incoming), deny (outgoing), disabled (routed)
```

**Remediation**:

```
Run the following commands to implement a default deny policy:
# ufw default deny incoming
# ufw default deny outgoing
# ufw default deny routed
```

---

### 4.3.4 — 4.3.4 Ensure a nftables table exists (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Tables hold chains. Each table only has one address family and only applies to packets
of this family. Tables can have one of five families.

**Rationale**:

nftables doesn't have any default tables. Without a table being built, nftables will not
filter network traffic.
Impact:
Adding rules to a running nftables can cause loss of connectivity to the system

**Audit guide**:

```
Run the following command to verify that a nftables table exists:
# nft list tables
Return should include a list of nftables:
Example:
table inet filter
```

**Remediation**:

```
Run the following command to create a table in nftables
# nft create table inet <table name>
Example:
# nft create table inet filter
```

---

### 4.3.5 — 4.3.5 Ensure nftables base chains exist (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Chains are containers for rules. They exist in two kinds, base chains and regular chains.
A base chain is an entry point for packets from the networking stack, a regular chain
may be used as jump target and is used for better rule organization.

**Rationale**:

If a base chain doesn't exist with a hook for input, forward, and delete, packets that
would flow through those chains will not be touched by nftables.
Impact:
If configuring nftables over ssh, creating a base chain with a policy of drop will
cause loss of connectivity.
Ensure that a rule allowing ssh has been added to the base chain prior to setting the
base chain's policy to drop

**Audit guide**:

```
Run the following commands and verify that base chains exist for INPUT.
# nft list ruleset | grep 'hook input'
type filter hook input priority 0;
Run the following commands and verify that base chains exist for FORWARD.
# nft list ruleset | grep 'hook forward'
type filter hook forward priority 0;
Run the following commands and verify that base chains exist for OUTPUT.
# nft list ruleset | grep 'hook output'
type filter hook output priority 0;
```

**Remediation**:

```
Run the following command to create the base chains:
# nft create chain inet <table name> <base chain name> { type filter hook
<(input|forward|output)> priority 0 \; }
Example:
# nft create chain inet filter input { type filter hook input priority 0 \; }
# nft create chain inet filter forward { type filter hook forward priority 0
\; }
# nft create chain inet filter output { type filter hook output priority 0 \;
}
```

---

### 4.3.8 — 4.3.8 Ensure nftables default deny firewall policy (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Base chain policy is the default verdict that will be applied to packets reaching the end
of the chain.

**Rationale**:

There are two policies: accept (Default) and drop. If the policy is set to accept, the
firewall will accept any packet that is not configured to be denied and the packet will
continue transversing the network stack.
It is easier to allow list acceptable usage than to deny list unacceptable usage.
Note:
• Allow port 22(ssh) needs to be updated to only allow systems requiring ssh
connectivity to connect, as per site policy.
• Changing firewall settings while connected over network can result in being
locked out of the system.
Impact:
If configuring nftables over ssh, creating a base chain with a policy of drop will cause
loss of connectivity.
Ensure that a rule allowing ssh has been added to the base chain prior to setting the
base chain's policy to drop

**Audit guide**:

```
Run the following commands and verify that base chains contain a policy of DROP.
# nft list ruleset | grep 'hook input'
type filter hook input priority 0; policy drop;
# nft list ruleset | grep 'hook forward'
type filter hook forward priority 0; policy drop;
# nft list ruleset | grep 'hook output'
type filter hook output priority 0; policy drop;
```

**Remediation**:

```
Run the following command for the base chains with the input, forward, and output
hooks to implement a default DROP policy:
# nft chain <table family> <table name> <chain name> { policy drop \; }
Example:
# nft chain inet filter input { policy drop \; }
# nft chain inet filter forward { policy drop \; }
# nft chain inet filter output { policy drop \; }
Default Value:
accept
```

---

### 4.3.10 — 4.3.10 Ensure nftables rules are permanent (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

nftables is a subsystem of the Linux kernel providing filtering and classification of
network packets/datagrams/frames.
The nftables service reads the /etc/nftables.conf file for a nftables file or files to
include in the nftables ruleset.
A nftables ruleset containing the input, forward, and output base chains allow network
traffic to be filtered.
Note: Saving the script and following the instruction in the Configure nftables section
overview will implement the rules in the configure nftable section, open port 22(ssh)
from anywhere, and applies nftables ruleset on boot.

**Rationale**:

Changes made to nftables ruleset only affect the live system, you will also need to
configure the nftables ruleset to apply on boot

**Audit guide**:

```
Run the following commands to verify that input, forward, and output base chains are
configured to be applied to a nftables ruleset on boot:
Run the following command to verify the input base chain:
# [ -n "$(grep -E '^\s*include' /etc/nftables.conf)" ] && awk '/hook
input/,/}/' $(awk '$1 ~ /^\s*include/ { gsub("\"","",$2);print $2 }'
/etc/nftables.conf)
Output should be similar to:
type filter hook input priority 0; policy drop;
# Ensure loopback traffic is configured
iif "lo" accept
ip saddr 127.0.0.0/8 counter packets 0 bytes 0 drop
ip6 saddr ::1 counter packets 0 bytes 0 drop
# Ensure established connections are configured
ip protocol tcp ct state established accept
ip protocol udp ct state established accept
# Accept port 22(SSH) traffic from anywhere
tcp dport ssh accept
Review the input base chain to ensure that it follows local site policy
Run the following command to verify the forward base chain:
# [ -n "$(grep -E '^\s*include' /etc/nftables.conf)" ] && awk '/hook
forward/,/}/' $(awk '$1 ~ /^\s*include/ { gsub("\"","",$2);print $2 }'
/etc/nftables.conf)
Output should be similar to:
# Base chain for hook forward named forward (Filters forwarded
network packets)
chain forward {
type filter hook forward priority 0; policy drop;
}
Review the forward base chain to ensure that it follows local site policy.
Run the following command to verify the forward base chain:
# [ -n "$(grep -E '^\s*include' /etc/nftables.conf)" ] && awk '/hook
output/,/}/' $(awk '$1 ~ /^\s*include/ { gsub("\"","",$2);print $2 }'
/etc/nftables.conf)
Output should be similar to:
# Base chain for hook output named output (Filters outbound network
packets)
chain output {
type filter hook output priority 0; policy drop;
# Ensure outbound and established connections are configured
ip protocol tcp ct state established,related,new accept
ip protocol udp ct state established,related,new accept
}
Review the output base chain to ensure that it follows local site policy.
```

**Remediation**:

```
Edit the /etc/nftables.conf file and un-comment or add a line with include
<Absolute path to nftables rules file> for each nftables file you want included
in the nftables ruleset on boot
Example:
# vi /etc/nftables.conf
Add the line:
include "/etc/nftables.rules"
```

---

### 4.4.2.1 — 4.4.2.1 Ensure iptables default deny firewall policy (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

A default deny all policy on connections ensures that any unconfigured network usage
will be rejected.
Notes:
• Changing firewall settings while connected over network can result in being
locked out of the system
• Remediation will only affect the active system firewall, be sure to configure the
default policy in your firewall management to apply on boot as well

**Rationale**:

With a default accept policy the firewall will accept any packet that is not configured to
be denied. It is easier to allow list acceptable usage than to deny list unacceptable
usage.

**Audit guide**:

```
Run the following command and verify that the policy for the INPUT , OUTPUT , and
FORWARD chains is DROP or REJECT :
# iptables -L
Chain INPUT (policy DROP)
Chain FORWARD (policy DROP)
Chain OUTPUT (policy DROP)
```

**Remediation**:

```
Run the following commands to implement a default DROP policy:
# iptables -P INPUT DROP
# iptables -P OUTPUT DROP
# iptables -P FORWARD DROP
```

---

### 4.4.2.2 — 4.4.2.2 Ensure iptables loopback traffic is configured (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Configure the loopback interface to accept traffic. Configure all other interfaces to deny
traffic to the loopback network (127.0.0.0/8).
Note:
• Changing firewall settings while connected over network can result in being
locked out of the system
• Remediation will only affect the active system firewall, be sure to configure the
default policy in your firewall management to apply on boot as well

**Rationale**:

Loopback traffic is generated between processes on machine and is typically critical to
the operation of the system. The loopback interface is the only place that loopback
network (127.0.0.0/8) traffic should be seen, all other interfaces should ignore traffic on
this network as an anti-spoofing measure.

**Audit guide**:

```
Run the following commands and verify output includes the listed rules in order (pkts
and bytes counts may differ, prot may be all or 0):
# iptables -L INPUT -v -n
Chain INPUT (policy DROP 0 packets, 0 bytes)
pkts bytes target prot opt in out source
destination
0 0 ACCEPT all -- lo * 0.0.0.0/0 0.0.0.0/0
0 0 DROP all -- * * 127.0.0.0/8 0.0.0.0/0
# iptables -L OUTPUT -v -n
Chain OUTPUT (policy DROP 0 packets, 0 bytes)
pkts bytes target prot opt in out source
destination
0 0 ACCEPT all -- * lo 0.0.0.0/0 0.0.0.0/0
```

**Remediation**:

```
Run the following commands to implement the loopback rules:
# iptables -A INPUT -i lo -j ACCEPT
# iptables -A OUTPUT -o lo -j ACCEPT
# iptables -A INPUT -s 127.0.0.0/8 -j DROP
```

---

### 4.4.2.4 — 4.4.2.4 Ensure iptables firewall rules exist for all open ports (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Any ports that have been opened on non-loopback addresses need firewall rules to
govern traffic.
Notes:
• Changing firewall settings while connected over network can result in being
locked out of the system
• Remediation will only affect the active system firewall, be sure to configure the
default policy in your firewall management to apply on boot as well
• The remediation command opens up the port to traffic from all sources. Consult
iptables documentation and set any restrictions in compliance with site policy

**Rationale**:

Without a firewall rule configured for open ports default firewall policy will drop all
packets to these ports.

**Audit guide**:

```
Run the following command to determine open ports:
# ss -4tuln
Netid State Recv-Q Send-Q Local Address:Port Peer
Address:Port
udp UNCONN 0 0 *:68
*:*
udp UNCONN 0 0 *:123
*:*
tcp LISTEN 0 128 *:22
*:*
Run the following command to determine firewall rules:
# iptables -L INPUT -v -n
Chain INPUT (policy DROP 0 packets, 0 bytes)
pkts bytes target prot opt in out source
destination
0 0 ACCEPT all -- lo * 0.0.0.0/0 0.0.0.0/0
0 0 DROP all -- * * 127.0.0.0/8 0.0.0.0/0
0 0 ACCEPT tcp -- * * 0.0.0.0/0 0.0.0.0/0
tcp dpt:22 state NEW
Verify all open ports listening on non-localhost addresses have at least one firewall rule.
The last line identified by the tcp dpt:22 state NEW identifies it as a firewall rule for
new connections on tcp port 22.
```

**Remediation**:

```
For each port identified in the audit which does not have a firewall rule establish a
proper rule for accepting inbound connections:
# iptables -A INPUT -p <protocol> --dport <port> -m state --state NEW -j
ACCEPT
```

---

### 5.1.4 — 5.1.4 Ensure sshd access is configured (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

There are several options available to limit which users and group can access the
system via SSH. It is recommended that at least one of the following options be
leveraged:
• AllowUsers:
o The AllowUsers variable gives the system administrator the option of
allowing specific users to ssh into the system. The list consists of space
separated user names. Numeric user IDs are not recognized with this
variable. If a system administrator wants to restrict user access further by
only allowing the allowed users to log in from a particular host, the entry
can be specified in the form of user@host.
• AllowGroups:
o The AllowGroups variable gives the system administrator the option of
allowing specific groups of users to ssh into the system. The list consists
of space separated group names. Numeric group IDs are not recognized
with this variable.
• DenyUsers:
o The DenyUsers variable gives the system administrator the option of
denying specific users to ssh into the system. The list consists of space
separated user names. Numeric user IDs are not recognized with this
variable. If a system administrator wants to restrict user access further by
specifically denying a user's access from a particular host, the entry can
be specified in the form of user@host.
• DenyGroups:
o The DenyGroups variable gives the system administrator the option of
denying specific groups of users to ssh into the system. The list consists
of space separated group names. Numeric group IDs are not recognized
with this variable.

**Rationale**:

Restricting which users can remotely access the system via SSH will help ensure that
only authorized users access the system.

**Audit guide**:

```
Run the following command and verify the output:
# sshd -T | grep -Pi -- '^\h*(allow|deny)(users|groups)\h+\H+'
Verify that the output matches at least one of the following lines:
allowusers <userlist>
-OR-
allowgroups <grouplist>
-OR-
denyusers <userlist>
-OR-
denygroups <grouplist>
Review the list(s) to ensure included users and/or groups follow local site policy
- IF - Match set statements are used in your environment, specify the connection
parameters to use for the -T extended test mode and run the audit to verify the setting
is not incorrectly configured in a match block
Example additional audit needed for a match block for the user sshuser:
# sshd -T -C user=sshuser | grep -Pi --
'^\h*(allow|deny)(users|groups)\h+\H+'
Note: If provided, any Match directives in the configuration file that would apply are
applied before the configuration is written to standard output. The connection
parameters are supplied as keyword=value pairs and may be supplied in any order,
either with multiple -C options or as a comma-separated list. The keywords are addr
(source address), user (user), host (resolved source host name), laddr (local
address), lport (local port number), and rdomain (routing domain).
```

**Remediation**:

```
Edit the /etc/ssh/sshd_config file to set one or more of the parameters above any
Include and Match set statements as follows:
AllowUsers <userlist>
- AND/OR -
AllowGroups <grouplist>
Note:
• First occurrence of a option takes precedence, Match set statements
withstanding. If Include locations are enabled, used, and order of precedence is
understood in your environment, the entry may be created in a .conf file in an
Include directory.
• Be advised that these options are "ANDed" together. If both AllowUsers and
AllowGroups are set, connections will be limited to the list of users that are also
a member of an allowed group. It is recommended that only one be set for clarity
and ease of administration.
• It is easier to manage an allow list than a deny list. In a deny list, you could
potentially add a user or group and forget to add it to the deny list.
Default Value:
None
```

---

### 5.1.14 — 5.1.14 Ensure sshd LogLevel is configured (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

SSH provides several logging levels with varying amounts of verbosity. The DEBUG
options are specifically not recommended other than strictly for debugging SSH
communications. These levels provide so much data that it is difficult to identify
important security information, and may violate the privacy of users.

**Rationale**:

The INFO level is the basic level that only records login activity of SSH users. In many
situations, such as Incident Response, it is important to determine when a particular
user was active on a system. The logout record can eliminate those users who
disconnected, which helps narrow the field.
The VERBOSE level specifies that login and logout activity as well as the key fingerprint
for any SSH key used for login will be logged. This information is important for SSH key
management, especially in legacy environments.

**Audit guide**:

```
Run the following command and verify that output matches loglevel VERBOSE or
loglevel INFO:
# sshd -T | grep loglevel
loglevel VERBOSE
- OR -
loglevel INFO
- IF - Match set statements are used in your environment, specify the connection
parameters to use for the -T extended test mode and run the audit to verify the setting
is not incorrectly configured in a match block
Example additional audit needed for a match block for the user sshuser:
# sshd -T -C user=sshuser | grep loglevel
Note: If provided, any Match directives in the configuration file that would apply are
applied before the configuration is written to standard output. The connection
parameters are supplied as keyword=value pairs and may be supplied in any order,
either with multiple -C options or as a comma-separated list. The keywords are addr
(source address), user (user), host (resolved source host name), laddr (local
address), lport (local port number), and rdomain (routing domain)
```

**Remediation**:

```
Edit the /etc/ssh/sshd_config file to set the LogLevel parameter to VERBOSE or
INFO above any Include and Match entries as follows:
LogLevel VERBOSE
- OR -
LogLevel INFO
Note: First occurrence of an option takes precedence, Match set statements
withstanding. If Include locations are enabled, used, and order of precedence is
understood in your environment, the entry may be created in a file in Include location.
Default Value:
LogLevel INFO
```

---

### 5.2.6 — 5.2.6 Ensure sudo authentication timeout is configured correctly (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

sudo caches used credentials for a default of 15 minutes. This is for ease of use when
there are multiple administrative tasks to perform. The timeout can be modified to suit
local security policies.
This default is distribution specific. See audit section for further information.

**Rationale**:

Setting a timeout value reduces the window of opportunity for unauthorized privileged
access to another user.

**Audit guide**:

```
Ensure that the caching timeout is no more than 15 minutes.
Example:
# grep -roP "timestamp_timeout=\K[0-9]*" /etc/sudoers*
If there is no timestamp_timeout configured in /etc/sudoers* then the default is 15
minutes. This default can be checked with:
# sudo -V | grep "Authentication timestamp timeout:"
Note: A value of -1 means that the timeout is disabled. Depending on the configuration
of the timestamp_type, this could mean for all terminals / processes of that user and
not just that one single terminal session.
```

**Remediation**:

```
If the currently configured timeout is larger than 15 minutes, edit the file listed in the
audit section with visudo -f <PATH TO FILE> and modify the entry
timestamp_timeout= to 15 minutes or less as per your site policy. The value is in
minutes. This particular entry may appear on it's own, or on the same line as
env_reset. See the following two examples:
Defaults env_reset, timestamp_timeout=15
Defaults timestamp_timeout=15
Defaults env_reset
```

---

### 5.3.1.1 — 5.3.1.1 Ensure latest version of pam is installed (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

Updated versions of PAM include additional functionality

**Rationale**:

To ensure the system has full functionality and access to the options covered by this
Benchmark the latest version of libpam-runtime should be installed on the system

**Audit guide**:

```
Run the following command to verify the version of libpam-runtime on the system:
# dpkg-query -s libpam-runtime | grep -P -- '^(Status|Version)\b'
The output should be similar to:
Status: install ok installed
Version: 1.5.3-5
```

**Remediation**:

```
- IF - the version of libpam-runtime on the system is less than version 1.5.3-5:
Run the following command to update to the latest version of PAM:
# apt upgrade libpam-runtime
5.3.1.2 Ensure libpam-modules is installed (Automated)
```

---

### 5.4.1.6 — 5.4.1.6 Ensure all users last password change date is in the past (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

All users should have a password change date in the past.

**Rationale**:

If a user's recorded password change date is in the future, then they could bypass any
set password expiration.

**Audit guide**:

```
Run the following command and verify nothing is returned
{
while IFS= read -r l_user; do
l_change=$(date -d "$(chage --list $l_user | grep '^Last password
change' | cut -d: -f2 | grep -v 'never$')" +%s)
if [[ "$l_change" -gt "$(date +%s)" ]]; then
echo "User: \"$l_user\" last password change was \"$(chage --list
$l_user | grep '^Last password change' | cut -d: -f2)\""
fi
done < <(awk -F: '$2~/^\$.+\$/{print $1}' /etc/shadow)
}
```

**Remediation**:

```
Investigate any users with a password change date in the future and correct them.
Locking the account, expiring the password, or resetting the password manually may be
appropriate.
```

---

### 5.4.2.2 — 5.4.2.2 Ensure root is the only GID 0 account (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

The usermod command can be used to specify which group the root account belongs
to. This affects permissions of files that are created by the root account.

**Rationale**:

Using GID 0 for the root account helps prevent root -owned files from accidentally
becoming accessible to non-privileged users.

**Audit guide**:

```
Run the following command to verify the root user's primary GID is 0, and no other
user's have GID 0 as their primary GID:
# awk -F: '($1 !~ /^(sync|shutdown|halt|operator)/ && $4=="0") {print
$1":"$4}' /etc/passwd
root:0
Note: User's: sync, shutdown, halt, and operator are excluded from the check for other
user's with GID 0
```

**Remediation**:

```
Run the following command to set the root user's GID to 0:
# usermod -g 0 root
Run the following command to set the root group's GID to 0:
# groupmod -g 0 root
Remove any users other than the root user with GID 0 or assign them a new GID if
appropriate.
```

---

### 5.4.2.3 — 5.4.2.3 Ensure group root is the only GID 0 group (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

The groupmod command can be used to specify which group the root group belongs
to. This affects permissions of files that are group owned by the root group.

**Rationale**:

Using GID 0 for the root group helps prevent root group owned files from accidentally
becoming accessible to non-privileged users.

**Audit guide**:

```
Run the following command to verify no group other than root is assigned GID 0:
# awk -F: '$3=="0"{print $1":"$3}' /etc/group
root:0
```

**Remediation**:

```
Run the following command to set the root group's GID to 0:
# groupmod -g 0 root
Remove any groups other than the root group with GID 0 or assign them a new GID if
appropriate.
```

---

### 5.4.2.4 — 5.4.2.4 Ensure root account access is controlled (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

There are a number of methods to access the root account directly. Without a password
set any user would be able to gain access and thus control over the entire system.

**Rationale**:

Access to root should be secured at all times.
Impact:
If there are any automated processes that relies on access to the root account without
authentication, they will fail after remediation.

**Audit guide**:

```
Run the following command to verify that either the root user's password is set or the
root user's account is locked:
# passwd -S root | awk '$2 ~ /^(P|L)/ {print "User: \"" $1 "\" Password is
status: " $2}'
Verify the output is either:
User: "root" Password is status: P
- OR -
User: "root" Password is status: L
Note:
• P - Password is set
• L - Password is locked
```

**Remediation**:

```
Run the following command to set a password for the root user:
# passwd root
- OR -
Run the following command to lock the root user account:
# usermod -L root
```

---

### 5.4.3.2 — 5.4.3.2 Ensure default user shell timeout is configured (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

TMOUT is an environmental setting that determines the timeout of a shell in seconds.
• TMOUT=n - Sets the shell timeout to n seconds. A setting of TMOUT=0 disables
timeout.
• readonly TMOUT- Sets the TMOUT environmental variable as readonly,
preventing unwanted modification during run-time.
• export TMOUT - exports the TMOUT variable
System Wide Shell Configuration Files:
• /etc/profile - used to set system wide environmental variables on users
shells. The variables are sometimes the same ones that are in the
.bash_profile, however this file is used to set an initial PATH or PS1 for all
shell users of the system. is only executed for interactive login shells, or
shells executed with the --login parameter.
• /etc/profile.d - /etc/profile will execute the scripts within
/etc/profile.d/*.sh. It is recommended to place your configuration in a shell
script within /etc/profile.d to set your own system wide environmental
variables.
• /etc/bashrc - System wide version of .bashrc. In Fedora derived distributions,
/etc/bashrc also invokes /etc/profile.d/*.sh if non-login shell, but redirects
output to /dev/null if non-interactive. Is only executed for interactive shells
or if BASH_ENV is set to /etc/bashrc.

**Rationale**:

Setting a timeout value reduces the window of opportunity for unauthorized user access
to another user's shell session that has been left unattended. It also ends the inactive
session and releases the resources associated with that session.

**Audit guide**:

```
Run the following script to verify that TMOUT is configured to: include a timeout of no
more than 900 seconds, to be readonly, to be exported, and is not being changed to
a longer timeout.
#!/usr/bin/env bash
{
output1="" output2=""
[ -f /etc/bashrc ] && BRC="/etc/bashrc"
for f in "$BRC" /etc/profile /etc/profile.d/*.sh ; do
grep -Pq '^\s*([^#]+\s+)?TMOUT=(900|[1-8][0-9][0-9]|[1-9][0-9]|[1-
9])\b' "$f" && grep -Pq
'^\s*([^#]+;\s*)?readonly\s+TMOUT(\s+|\s*;|\s*$|=(900|[1-8][0-9][0-9]|[1-
9][0-9]|[1-9]))\b' "$f" && grep -Pq
'^\s*([^#]+;\s*)?export\s+TMOUT(\s+|\s*;|\s*$|=(900|[1-8][0-9][0-9]|[1-9][0-
9]|[1-9]))\b' "$f" &&
output1="$f"
done
grep -Pq '^\s*([^#]+\s+)?TMOUT=(9[0-9][1-9]|9[1-9][0-9]|0+|[1-9]\d{3,})\b'
/etc/profile /etc/profile.d/*.sh "$BRC" && output2=$(grep -Ps
'^\s*([^#]+\s+)?TMOUT=(9[0-9][1-9]|9[1-9][0-9]|0+|[1-9]\d{3,})\b'
/etc/profile /etc/profile.d/*.sh $BRC)
if [ -n "$output1" ] && [ -z "$output2" ]; then
echo -e "\nPASSED\n\nTMOUT is configured in: \"$output1\"\n"
else
[ -z "$output1" ] && echo -e "\nFAILED\n\nTMOUT is not configured\n"
[ -n "$output2" ] && echo -e "\nFAILED\n\nTMOUT is incorrectly
configured in: \"$output2\"\n"
fi
}
```

**Remediation**:

```
Review /etc/bashrc, /etc/profile, and all files ending in *.sh in the
/etc/profile.d/ directory and remove or edit all TMOUT=_n_ entries to follow local site
policy. TMOUT should not exceed 900 or be equal to 0.
Configure TMOUT in one of the following files:
• A file in the /etc/profile.d/ directory ending in .sh
• /etc/profile
• /etc/bashrc
TMOUT configuration examples:
• As multiple lines:
TMOUT=900
readonly TMOUT
export TMOUT
• As a single line:
readonly TMOUT=900 ; export TMOUT
Additional Information:
The audit and remediation in this recommendation apply to bash and shell. If other
shells are supported on the system, it is recommended that their configuration files also
are checked. Other methods of setting a timeout exist for other shells not covered here.
Ensure that the timeout conforms to your local policy.
```

---

### 6.2.2.3 — 6.2.2.3 Ensure system is disabled when audit logs are full (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

The auditd daemon can be configured to halt the system or put the system in single
user mode, if no free space is available or an error is detected on the partition that holds
the audit log files.
The disk_full_action parameter tells the system what action to take when no free
space is available on the partition that holds the audit log files. Valid values are ignore,
syslog, rotate, exec, suspend, single, and halt.
• ignore, the audit daemon will issue a syslog message but no other action is
taken
• syslog, the audit daemon will issue a warning to syslog
• rotate, the audit daemon will rotate logs, losing the oldest to free up space
• exec, /path-to-script will execute the script. You cannot pass parameters to the
script. The script is also responsible for telling the auditd daemon to resume
logging once its completed its action
• suspend, the audit daemon will stop writing records to the disk
• single, the audit daemon will put the computer system in single user mode
• halt, the audit daemon will shut down the system
The disk_error_action parameter tells the system what action to take when an error
is detected on the partition that holds the audit log files. Valid values are ignore,
syslog, exec, suspend, single, and halt.
• ignore, the audit daemon will not take any action
• syslog, the audit daemon will issue no more than 5 consecutive warnings to
syslog
• exec, /path-to-script will execute the script. You cannot pass parameters to the
script
• suspend, the audit daemon will stop writing records to the disk
• single, the audit daemon will put the computer system in single user mode
• halt, the audit daemon will shut down the system

**Rationale**:

In high security contexts, the risk of detecting unauthorized access or nonrepudiation
exceeds the benefit of the system's availability.
Impact:
disk_full_action parameter:
• Set to halt - the auditd daemon will shutdown the system when the disk
partition containing the audit logs becomes full.
• Set to single - the auditd daemon will put the computer system in single user
mode when the disk partition containing the audit logs becomes full.
disk_error_action parameter:
• Set to halt - the auditd daemon will shutdown the system when an error is
detected on the partition that holds the audit log files.
• Set to single - the auditd daemon will put the computer system in single user
mode when an error is detected on the partition that holds the audit log files.
• Set to syslog - the auditd daemon will issue no more than 5 consecutive
warnings to syslog when an error is detected on the partition that holds the audit
log files.

**Audit guide**:

```
Run the following command and verify the disk_full_action is set to either halt or
single:
# grep -Pi -- '^\h*disk_full_action\h*=\h*(halt|single)\b'
/etc/audit/auditd.conf
disk_full_action = <halt|single>
Run the following command and verify the disk_error_action is set to syslog,
single, or halt:
# grep -Pi -- '^\h*disk_error_action\h*=\h*(syslog|single|halt)\b'
/etc/audit/auditd.conf
disk_error_action = <syslog|single|halt>
```

**Remediation**:

```
Set one of the following parameters in /etc/audit/auditd.conf depending on your
local security policies.
disk_full_action = <halt|single>
disk_error_action = <syslog|single|halt>
Example:
disk_full_action = halt
disk_error_action = halt
```

---

### 6.2.3.1 — 6.2.3.1 Ensure changes to system administration scope (sudoers) (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor scope changes for system administrators. If the system has been properly
configured to force system administrators to log in as themselves first and then use the
sudo command to execute privileged commands, it is possible to monitor changes in
scope. The file /etc/sudoers, or files in /etc/sudoers.d, will be written to when the
file(s) or related attributes have changed. The audit records will be tagged with the
identifier "scope".

**Rationale**:

Changes in the /etc/sudoers and /etc/sudoers.d files can indicate that an
unauthorized change has been made to the scope of system administrator activity.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# awk '/^ *-w/ \
&&/\/etc\/sudoers/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d -p wa -k scope
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk '/^ *-w/ \
&&/\/etc\/sudoers/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
Verify the output matches:
-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d -p wa -k scope
```

**Remediation**:

```
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor scope changes for system administrators.
Example:
# printf "
-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d -p wa -k scope
" >> /etc/audit/rules.d/50-scope.rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.2 — 6.2.3.2 Ensure actions as another user are always logged (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

sudo provides users with temporary elevated privileges to perform operations, either as
the superuser or another user.

**Rationale**:

Creating an audit log of users with temporary elevated privileges and the operation(s)
they performed is essential to reporting. Administrators will want to correlate the events
written to the audit trail with the records written to sudo's logfile to verify if unauthorized
commands have been executed.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&(/ -C *euid!=uid/||/ -C *uid!=euid/) \
&&/ -S *execve/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-a always,exit -F arch=b64 -C euid!=uid -F auid!=unset -S execve -k
user_emulation
-a always,exit -F arch=b32 -C euid!=uid -F auid!=unset -S execve -k
user_emulation
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&(/ -C *euid!=uid/||/ -C *uid!=euid/) \
&&/ -S *execve/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
Verify the output matches:
-a always,exit -F arch=b64 -S execve -C uid!=euid -F auid!=-1 -F
key=user_emulation
-a always,exit -F arch=b32 -S execve -C uid!=euid -F auid!=-1 -F
key=user_emulation
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor elevated privileges.
Example:
# printf "
-a always,exit -F arch=b64 -C euid!=uid -F auid!=unset -S execve -k
user_emulation
-a always,exit -F arch=b32 -C euid!=uid -F auid!=unset -S execve -k
user_emulation
" >> /etc/audit/rules.d/50-user_emulation.rules
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.3 — 6.2.3.3 Ensure events that modify the sudo log file are collected (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor the sudo log file. If the system has been properly configured to disable the use
of the su command and force all administrators to have to log in first and then use sudo
to execute privileged commands, then all administrator commands will be logged to
/var/log/sudo.log . Any time a command is executed, an audit event will be
triggered as the /var/log/sudo.log file will be opened for write and the executed
administration command will be written to the log.

**Rationale**:

Changes in /var/log/sudo.log indicate that an administrator has executed a
command or the log file itself has been tampered with. Administrators will want to
correlate the events written to the audit trail with the records written to
/var/log/sudo.log to verify if unauthorized commands have been executed.

**Audit guide**:

```
Note: This recommendation requires that the sudo logfile is configured. See guidance
provided in the recommendation "Ensure sudo log file exists"
On disk configuration
Run the following command to check the on disk rules:
# {
SUDO_LOG_FILE=$(grep -r logfile /etc/sudoers* | sed -e 's/.*logfile=//;s/,?
.*//' -e 's/"//g' -e 's|/|\\/|g')
[ -n "${SUDO_LOG_FILE}" ] && awk "/^ *-w/ \
&&/"${SUDO_LOG_FILE}"/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'SUDO_LOG_FILE' is unset.\n"
}
Verify output of matches:
-w /var/log/sudo.log -p wa -k sudo_log_file
Running configuration
Run the following command to check loaded rules:
# {
SUDO_LOG_FILE=$(grep -r logfile /etc/sudoers* | sed -e 's/.*logfile=//;s/,?
.*//' -e 's/"//g' -e 's|/|\\/|g')
[ -n "${SUDO_LOG_FILE}" ] && auditctl -l | awk "/^ *-w/ \
&&/"${SUDO_LOG_FILE}"/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'SUDO_LOG_FILE' is unset.\n"
}
Verify output matches:
-w /var/log/sudo.log -p wa -k sudo_log_file
```

**Remediation**:

```
Note: This recommendation requires that the sudo logfile is configured. See guidance
provided in the recommendation "Ensure sudo log file exists"
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor events that modify the sudo log file.
Example:
# {
SUDO_LOG_FILE=$(grep -r logfile /etc/sudoers* | sed -e 's/.*logfile=//;s/,?
.*//' -e 's/"//g')
[ -n "${SUDO_LOG_FILE}" ] && printf "
-w ${SUDO_LOG_FILE} -p wa -k sudo_log_file
" >> /etc/audit/rules.d/50-sudo.rules || printf "ERROR: Variable
'SUDO_LOG_FILE' is unset.\n"
}
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
Additional Information:
Potential reboot required
If the auditing configuration is locked (-e 2), then augenrules will not warn in any way
that rules could not be loaded into the running configuration. A system reboot will be
required to load the rules into the running configuration.
System call structure
For performance (man 7 audit.rules) reasons it is preferable to have all the system
calls on one line. However, your configuration may have them on one line each or some
other combination. This is important to understand for both the auditing and remediation
sections as the examples given are optimized for performance as per the man page.
```

---

### 6.2.3.4 — 6.2.3.4 Ensure events that modify date and time information are (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Capture events where the system date and/or time has been modified. The parameters
in this section are set to determine if the;
• adjtimex - tune kernel clock
• settimeofday - set time using timeval and timezone structures
• stime - using seconds since 1/1/1970
• clock_settime - allows for the setting of several internal clocks and timers
system calls have been executed. Further, ensure to write an audit record to the
configured audit log file upon exit, tagging the records with a unique identifier such as
"time-change".

**Rationale**:

Unexpected changes in system date and/or time could be a sign of malicious activity on
the system.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# {
awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&/ -S/ \
&&(/adjtimex/ \
||/settimeofday/ \
||/clock_settime/ ) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
awk '/^ *-w/ \
&&/\/etc\/localtime/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
}
Verify output of matches:
-a always,exit -F arch=b64 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b32 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b64 -S clock_settime -F a0=0x0 -k time-change
-a always,exit -F arch=b32 -S clock_settime -F a0=0x0 -k time-change
-w /etc/localtime -p wa -k time-change
Running configuration
Run the following command to check loaded rules:
# {
auditctl -l | awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&/ -S/ \
&&(/adjtimex/ \
||/settimeofday/ \
||/clock_settime/ ) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
auditctl -l | awk '/^ *-w/ \
&&/\/etc\/localtime/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
}
Verify the output includes:
-a always,exit -F arch=b64 -S adjtimex,settimeofday -F key=time-change
-a always,exit -F arch=b32 -S settimeofday,adjtimex -F key=time-change
-a always,exit -F arch=b64 -S clock_settime -F a0=0x0 -F key=time-change
-a always,exit -F arch=b32 -S clock_settime -F a0=0x0 -F key=time-change
-w /etc/localtime -p wa -k time-change
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor events that modify date and time information.
Example:
# printf "
-a always,exit -F arch=b64 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b32 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b64 -S clock_settime -F a0=0x0 -k time-change
-a always,exit -F arch=b32 -S clock_settime -F a0=0x0 -k time-change
-w /etc/localtime -p wa -k time-change
" >> /etc/audit/rules.d/50-time-change.rules
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.5 — 6.2.3.5 Ensure events that modify the system's network (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Record changes to network environment files or system calls. The below parameters
monitors the following system calls, and write an audit event on system call exit:
• sethostname - set the systems host name
• setdomainname - set the systems domain name
The files being monitored are:
• /etc/issue and /etc/issue.net - messages displayed pre-login
• /etc/hosts - file containing host names and associated IP addresses
• /etc/networks - symbolic names for networks
• /etc/network/ - directory containing network interface scripts and
configurations files
• /etc/netplan/ - central location for YAML networking configurations files

**Rationale**:

Monitoring system events that change network environments, such as sethostname
and setdomainname, helps identify unauthorized alterations to host and domain names,
which could compromise security settings reliant on these names. Changes to
/etc/hosts can signal unauthorized attempts to alter machine associations with IP
addresses, potentially redirecting users and processes to unintended destinations.
Surveillance of /etc/issue and /etc/issue.net is crucial to detect intruders inserting
false information to deceive users. Monitoring /etc/network/ reveals modifications to
network interfaces or scripts that may jeopardize system availability or security.
Additionally, tracking changes in the /etc/netplan/ directory ensures swift detection
of unauthorized adjustments to network configurations. All audit records should be
appropriately tagged for relevance

**Audit guide**:

```
On disk configuration
Run the following commands to check the on disk rules:
# awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&/ -S/ \
&&(/sethostname/ \
||/setdomainname/) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
# awk '/^ *-w/ \
&&(/\/etc\/issue/ \
||/\/etc\/issue.net/ \
||/\/etc\/hosts/ \
||/\/etc\/network/ \
||/\/etc\/netplan/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-a always,exit -F arch=b64 -S sethostname,setdomainname -k system-locale
-a always,exit -F arch=b32 -S sethostname,setdomainname -k system-locale
-w /etc/issue -p wa -k system-locale
-w /etc/issue.net -p wa -k system-locale
-w /etc/hosts -p wa -k system-locale
-w /etc/networks -p wa -k system-locale
-w /etc/network -p wa -k system-locale
-w /etc/netplan -p wa -k system-locale
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&/ -S/ \
&&(/sethostname/ \
||/setdomainname/) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
# auditctl -l | awk '/^ *-w/ \
&&(/\/etc\/issue/ \
||/\/etc\/issue.net/ \
||/\/etc\/hosts/ \
||/\/etc\/network/ \
||/\/etc\/netplan/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
Verify the output includes:
-a always,exit -F arch=b64 -S sethostname,setdomainname -F key=system-locale
-a always,exit -F arch=b32 -S sethostname,setdomainname -F key=system-locale
-w /etc/issue -p wa -k system-locale
-w /etc/issue.net -p wa -k system-locale
-w /etc/hosts -p wa -k system-locale
-w /etc/networks -p wa -k system-locale
-w /etc/network -p wa -k system-locale
-w /etc/netplan -p wa -k system-locale
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor events that modify the system's network environment.
Example:
# printf "
-a always,exit -F arch=b64 -S sethostname,setdomainname -k system-locale
-a always,exit -F arch=b32 -S sethostname,setdomainname -k system-locale
-w /etc/issue -p wa -k system-locale
-w /etc/issue.net -p wa -k system-locale
-w /etc/hosts -p wa -k system-locale
-w /etc/networks -p wa -k system-locale
-w /etc/network/ -p wa -k system-locale
-w /etc/netplan/ -p wa -k system-locale
" >> /etc/audit/rules.d/50-system_locale.rules
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.6 — 6.2.3.6 Ensure use of privileged commands are collected (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor privileged programs, those that have the setuid and/or setgid bit set on
execution, to determine if unprivileged users are running these commands.

**Rationale**:

Execution of privileged commands by non-privileged users could be an indication of
someone trying to gain unauthorized access to the system.
Impact:
Both the audit and remediation section of this recommendation will traverse all mounted
file systems that is not mounted with either noexec or nosuid mount options. If there
are large file systems without these mount options, such traversal will be significantly
detrimental to the performance of the system.
Before running either the audit or remediation section, inspect the output of the following
command to determine exactly which file systems will be traversed:
# findmnt -n -l -k -it $(awk '/nodev/ { print $2 }' /proc/filesystems | paste
-sd,) | grep -Pv "noexec|nosuid"
To exclude a particular file system due to adverse performance impacts, update the
audit and remediation sections by adding a sufficiently unique string to the grep
statement. The above command can be used to test the modified exclusions.

**Audit guide**:

```
On disk configuration
Run the following script to check on disk rules:
#!/usr/bin/env bash
{
for PARTITION in $(findmnt -n -l -k -it $(awk '/nodev/ { print $2 }'
/proc/filesystems | paste -sd,) | grep -Pv "noexec|nosuid" | awk '{print
$1}'); do
for PRIVILEGED in $(find "${PARTITION}" -xdev -perm /6000 -type f); do
grep -qr "${PRIVILEGED}" /etc/audit/rules.d && printf "OK:
'${PRIVILEGED}' found in auditing rules.\n" || printf "Warning:
'${PRIVILEGED}' not found in on disk configuration.\n"
done
done
}
Verify that all output is OK.
Running configuration
Run the following script to check loaded rules:
#!/usr/bin/env bash
{
RUNNING=$(auditctl -l)
[ -n "${RUNNING}" ] && for PARTITION in $(findmnt -n -l -k -it $(awk
'/nodev/ { print $2 }' /proc/filesystems | paste -sd,) | grep -Pv
"noexec|nosuid" | awk '{print $1}'); do
for PRIVILEGED in $(find "${PARTITION}" -xdev -perm /6000 -type f); do
printf -- "${RUNNING}" | grep -q "${PRIVILEGED}" && printf "OK:
'${PRIVILEGED}' found in auditing rules.\n" || printf "Warning:
'${PRIVILEGED}' not found in running configuration.\n"
done
done \
|| printf "ERROR: Variable 'RUNNING' is unset.\n"
}
Verify that all output is OK.
Special mount points
If there are any special mount points that are not visible by default from findmnt as per
the above audit, said file systems would have to be manually audited.
```

**Remediation**:

```
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor the use of privileged commands.
Example script:
#!/usr/bin/env bash
{
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
AUDIT_RULE_FILE="/etc/audit/rules.d/50-privileged.rules"
NEW_DATA=()
for PARTITION in $(findmnt -n -l -k -it $(awk '/nodev/ { print $2 }'
/proc/filesystems | paste -sd,) | grep -Pv "noexec|nosuid" | awk '{print
$1}'); do
readarray -t DATA < <(find "${PARTITION}" -xdev -perm /6000 -type f | awk
-v UID_MIN=${UID_MIN} '{print "-a always,exit -F path=" $1 " -F perm=x -F
auid>="UID_MIN" -F auid!=unset -k privileged" }')
for ENTRY in "${DATA[@]}"; do
NEW_DATA+=("${ENTRY}")
done
done
readarray &> /dev/null -t OLD_DATA < "${AUDIT_RULE_FILE}"
COMBINED_DATA=( "${OLD_DATA[@]}" "${NEW_DATA[@]}" )
printf '%s\n' "${COMBINED_DATA[@]}" | sort -u > "${AUDIT_RULE_FILE}"
}
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
Special mount points
If there are any special mount points that are not visible by default from just scanning /,
change the PARTITION variable to the appropriate partition and re-run the remediation.
```

---

### 6.2.3.7 — 6.2.3.7 Ensure unsuccessful file access attempts are collected (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor for unsuccessful attempts to access files. The following parameters are
associated with system calls that control files:
• creation - creat
• opening - open , openat
• truncation - truncate , ftruncate
An audit log record will only be written if all of the following criteria is met for the user
when trying to access a file:
• a non-privileged user (auid>=UID_MIN)
• is not a Daemon event (auid=4294967295/unset/-1)
• if the system call returned EACCES (permission denied) or EPERM (some other
permanent error associated with the specific system call)

**Rationale**:

Failed attempts to open, create or truncate files could be an indication that an individual
or process is trying to gain unauthorized access to the system.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&(/ -F *exit=-EACCES/||/ -F *exit=-EPERM/) \
&&/ -S/ \
&&/creat/ \
&&/open/ \
&&/truncate/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output includes:
-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-
EACCES -F auid>=1000 -F auid!=unset -k access
-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-
EPERM -F auid>=1000 -F auid!=unset -k access
-a always,exit -F arch=b32 -S creat,open,openat,truncate,ftruncate -F exit=-
EACCES -F auid>=1000 -F auid!=unset -k access
-a always,exit -F arch=b32 -S creat,open,openat,truncate,ftruncate -F exit=-
EPERM -F auid>=1000 -F auid!=unset -k access
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&(/ -F *exit=-EACCES/||/ -F *exit=-EPERM/) \
&&/ -S/ \
&&/creat/ \
&&/open/ \
&&/truncate/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output includes:
-a always,exit -F arch=b64 -S open,truncate,ftruncate,creat,openat -F exit=-
EACCES -F auid>=1000 -F auid!=-1 -F key=access
-a always,exit -F arch=b64 -S open,truncate,ftruncate,creat,openat -F exit=-
EPERM -F auid>=1000 -F auid!=-1 -F key=access
-a always,exit -F arch=b32 -S open,truncate,ftruncate,creat,openat -F exit=-
EACCES -F auid>=1000 -F auid!=-1 -F key=access
-a always,exit -F arch=b32 -S open,truncate,ftruncate,creat,openat -F exit=-
EPERM -F auid>=1000 -F auid!=-1 -F key=access
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor unsuccessful file access attempts.
Example:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-
EACCES -F auid>=${UID_MIN} -F auid!=unset -k access
-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-
EPERM -F auid>=${UID_MIN} -F auid!=unset -k access
-a always,exit -F arch=b32 -S creat,open,openat,truncate,ftruncate -F exit=-
EACCES -F auid>=${UID_MIN} -F auid!=unset -k access
-a always,exit -F arch=b32 -S creat,open,openat,truncate,ftruncate -F exit=-
EPERM -F auid>=${UID_MIN} -F auid!=unset -k access
" >> /etc/audit/rules.d/50-access.rules || printf "ERROR: Variable 'UID_MIN'
is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.8 — 6.2.3.8 Ensure events that modify user/group information are (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Record events affecting the modification of user or group information, including that of
passwords and old passwords if in use.
• /etc/group - system groups
• /etc/passwd - system users
• /etc/gshadow - encrypted password for each group
• /etc/shadow - system user passwords
• /etc/security/opasswd - storage of old passwords if the relevant PAM module
is in use
• /etc/nsswitch.conf - file configures how the system uses various databases
and name resolution mechanisms
• /etc/pam.conf - file determines the authentication services to be used, and the
order in which the services are used.
• /etc/pam.d - directory contains the PAM configuration files for each PAM-aware
application.
The parameters in this section will watch the files to see if they have been opened for
write or have had attribute changes (e.g. permissions) and tag them with the identifier
"identity" in the audit log file.

**Rationale**:

Unexpected changes to these files could be an indication that the system has been
compromised and that an unauthorized user is attempting to hide their activities or
compromise additional accounts.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# awk '/^ *-w/ \
&&(/\/etc\/group/ \
||/\/etc\/passwd/ \
||/\/etc\/gshadow/ \
||/\/etc\/shadow/ \
||/\/etc\/security\/opasswd/ \
||/\/etc\/nsswitch.conf/ \
||/\/etc\/pam.conf/ \
||/\/etc\/pam.d/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-w /etc/group -p wa -k identity
-w /etc/passwd -p wa -k identity
-w /etc/gshadow -p wa -k identity
-w /etc/shadow -p wa -k identity
-w /etc/security/opasswd -p wa -k identity
-w /etc/nsswitch.conf -p wa -k identity
-w /etc/pam.conf -p wa -k identity
-w /etc/pam.d -p wa -k identity
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk '/^ *-w/ \
&&(/\/etc\/group/ \
||/\/etc\/passwd/ \
||/\/etc\/gshadow/ \
||/\/etc\/shadow/ \
||/\/etc\/security\/opasswd/ \
||/\/etc\/nsswitch.conf/ \
||/\/etc\/pam.conf/ \
||/\/etc\/pam.d/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
Verify the output matches:
-w /etc/group -p wa -k identity
-w /etc/passwd -p wa -k identity
-w /etc/gshadow -p wa -k identity
-w /etc/shadow -p wa -k identity
-w /etc/security/opasswd -p wa -k identity
-w /etc/nsswitch.conf -p wa -k identity
-w /etc/pam.conf -p wa -k identity
-w /etc/pam.d -p wa -k identity
```

**Remediation**:

```
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor events that modify user/group information.
Example:
# printf "
-w /etc/group -p wa -k identity
-w /etc/passwd -p wa -k identity
-w /etc/gshadow -p wa -k identity
-w /etc/shadow -p wa -k identity
-w /etc/security/opasswd -p wa -k identity
-w /etc/nsswitch.conf -p wa -k identity
-w /etc/pam.conf -p wa -k identity
-w /etc/pam.d -p wa -k identity
" >> /etc/audit/rules.d/50-identity.rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.9 — 6.2.3.9 Ensure discretionary access control permission (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor changes to file permissions, attributes, ownership and group. The parameters in
this section track changes for system calls that affect file permissions and attributes.
The following commands and system calls effect the permissions, ownership and
various attributes of files.
• chmod
• fchmod
• fchmodat
• chown
• fchown
• fchownat
• lchown
• setxattr
• lsetxattr
• fsetxattr
• removexattr
• lremovexattr
• fremovexattr
In all cases, an audit record will only be written for non-system user ids and will ignore
Daemon events. All audit records will be tagged with the identifier "perm_mod."

**Rationale**:

Monitoring for changes in file attributes could alert a system administrator to activity that
could indicate intruder activity or policy violation.

**Audit guide**:

```
Note: Output showing all audited syscalls, e.g. (-a always,exit -F arch=b64 -S
chmod,fchmod,fchmodat,chmod,fchmod,fchmodat,setxattr,lsetxattr,fsetxattr,removexattr
,lremovexattr,fremovexattr -F auid>=1000 -F auid!=unset -F key=perm_mod) is also
acceptable. These have been separated by function on the displayed output for clarity.
On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -S/ \
&&/ -F *auid>=${UID_MIN}/ \
&&(/chmod/||/fchmod/||/fchmodat/ \
||/chown/||/fchown/||/fchownat/||/lchown/ \
||/setxattr/||/lsetxattr/||/fsetxattr/ \
||/removexattr/||/lremovexattr/||/fremovexattr/) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F arch=b64 -S chmod,fchmod,fchmodat -F auid>=1000 -F
auid!=unset -F key=perm_mod
-a always,exit -F arch=b64 -S chown,fchown,lchown,fchownat -F auid>=1000 -F
auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S chmod,fchmod,fchmodat -F auid>=1000 -F
auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S lchown,fchown,chown,fchownat -F auid>=1000 -F
auid!=unset -F key=perm_mod
-a always,exit -F arch=b64 -S
setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F
auid>=1000 -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S
setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F
auid>=1000 -F auid!=unset -F key=perm_mod
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -S/ \
&&/ -F *auid>=${UID_MIN}/ \
&&(/chmod/||/fchmod/||/fchmodat/ \
||/chown/||/fchown/||/fchownat/||/lchown/ \
||/setxattr/||/lsetxattr/||/fsetxattr/ \
||/removexattr/||/lremovexattr/||/fremovexattr/) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F arch=b64 -S chmod,fchmod,fchmodat -F auid>=1000 -F auid!=-1
-F key=perm_mod
-a always,exit -F arch=b64 -S chown,fchown,lchown,fchownat -F auid>=1000 -F
auid!=-1 -F key=perm_mod
-a always,exit -F arch=b32 -S chmod,fchmod,fchmodat -F auid>=1000 -F auid!=-1
-F key=perm_mod
-a always,exit -F arch=b32 -S lchown,fchown,chown,fchownat -F auid>=1000 -F
auid!=-1 -F key=perm_mod
-a always,exit -F arch=b64 -S
setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F
auid>=1000 -F auid!=-1 -F key=perm_mod
-a always,exit -F arch=b32 -S
setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F
auid>=1000 -F auid!=-1 -F key=perm_mod
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor discretionary access control permission modification
events.
Example:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F arch=b64 -S chmod,fchmod,fchmodat -F auid>=${UID_MIN} -F
auid!=unset -F key=perm_mod
-a always,exit -F arch=b64 -S chown,fchown,lchown,fchownat -F
auid>=${UID_MIN} -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S chmod,fchmod,fchmodat -F auid>=${UID_MIN} -F
auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S lchown,fchown,chown,fchownat -F
auid>=${UID_MIN} -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b64 -S
setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F
auid>=${UID_MIN} -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S
setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F
auid>=${UID_MIN} -F auid!=unset -F key=perm_mod
" >> /etc/audit/rules.d/50-perm_mod.rules || printf "ERROR: Variable
'UID_MIN' is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.10 — 6.2.3.10 Ensure successful file system mounts are collected (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor the use of the mount system call. The mount (and umount ) system call controls
the mounting and unmounting of file systems. The parameters below configure the
system to create an audit record when the mount system call is used by a non-
privileged user

**Rationale**:

It is highly unusual for a non privileged user to mount file systems to the system. While
tracking mount commands gives the system administrator evidence that external media
may have been mounted (based on a review of the source of the mount and confirming
it's an external media type), it does not conclusively indicate that data was exported to
the media. System administrators who wish to determine if data were exported, would
also have to track successful open, creat and truncate system calls requiring write
access to a file under the mount point of the external media file system. This could give
a fair indication that a write occurred. The only way to truly prove it, would be to track
successful writes to the external media. Tracking write system calls could quickly fill up
the audit log and is not recommended. Recommendations on configuration options to
track data export to media is beyond the scope of this document.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -S/ \
&&/mount/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F arch=b64 -S mount -F auid>=1000 -F auid!=unset -k mounts
-a always,exit -F arch=b32 -S mount -F auid>=1000 -F auid!=unset -k mounts
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -S/ \
&&/mount/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F arch=b64 -S mount -F auid>=1000 -F auid!=-1 -F key=mounts
-a always,exit -F arch=b32 -S mount -F auid>=1000 -F auid!=-1 -F key=mounts
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor successful file system mounts.
Example:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F arch=b32 -S mount -F auid>=$UID_MIN -F auid!=unset -k
mounts
-a always,exit -F arch=b64 -S mount -F auid>=$UID_MIN -F auid!=unset -k
mounts
" >> /etc/audit/rules.d/50-mounts.rules || printf "ERROR: Variable 'UID_MIN'
is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.11 — 6.2.3.11 Ensure session initiation information is collected (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor session initiation events. The parameters in this section track changes to the
files associated with session events.
• /var/run/utmp - tracks all currently logged in users.
• /var/log/wtmp - file tracks logins, logouts, shutdown, and reboot events.
• /var/log/btmp - keeps track of failed login attempts and can be read by
entering the command /usr/bin/last -f /var/log/btmp.
All audit records will be tagged with the identifier "session."

**Rationale**:

Monitoring these files for changes could alert a system administrator to logins occurring
at unusual hours, which could indicate intruder activity (i.e. a user logging in at a time
when they do not normally log in).

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# awk '/^ *-w/ \
&&(/\/var\/run\/utmp/ \
||/\/var\/log\/wtmp/ \
||/\/var\/log\/btmp/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-w /var/run/utmp -p wa -k session
-w /var/log/wtmp -p wa -k session
-w /var/log/btmp -p wa -k session
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk '/^ *-w/ \
&&(/\/var\/run\/utmp/ \
||/\/var\/log\/wtmp/ \
||/\/var\/log\/btmp/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
Verify the output matches:
-w /var/run/utmp -p wa -k session
-w /var/log/wtmp -p wa -k session
-w /var/log/btmp -p wa -k session
```

**Remediation**:

```
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor session initiation information.
Example:
# printf "
-w /var/run/utmp -p wa -k session
-w /var/log/wtmp -p wa -k session
-w /var/log/btmp -p wa -k session
" >> /etc/audit/rules.d/50-session.rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.12 — 6.2.3.12 Ensure login and logout events are collected (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor login and logout events. The parameters below track changes to files
associated with login/logout events.
• /var/log/lastlog - maintain records of the last time a user successfully logged
in.
• /var/run/faillock - directory maintains records of login failures via the
pam_faillock module.

**Rationale**:

Monitoring login/logout events could provide a system administrator with information
associated with brute force attacks against user logins.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# awk '/^ *-w/ \
&&(/\/var\/log\/lastlog/ \
||/\/var\/run\/faillock/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-w /var/log/lastlog -p wa -k logins
-w /var/run/faillock -p wa -k logins
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk '/^ *-w/ \
&&(/\/var\/log\/lastlog/ \
||/\/var\/run\/faillock/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
Verify the output matches:
-w /var/log/lastlog -p wa -k logins
-w /var/run/faillock -p wa -k logins
```

**Remediation**:

```
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor login and logout events.
Example:
# printf "
-w /var/log/lastlog -p wa -k logins
-w /var/run/faillock -p wa -k logins
" >> /etc/audit/rules.d/50-login.rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.13 — 6.2.3.13 Ensure file deletion events by users are collected (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor the use of system calls associated with the deletion or renaming of files and file
attributes. This configuration statement sets up monitoring for:
• unlink - remove a file
• unlinkat - remove a file attribute
• rename - rename a file
• renameat rename a file attribute system calls and tags them with the identifier
"delete".

**Rationale**:

Monitoring these calls from non-privileged users could provide a system administrator
with evidence that inappropriate removal of files and file attributes associated with
protected files is occurring. While this audit option will look at all events, system
administrators will want to look for specific privileged files that are being deleted or
altered.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -S/ \
&&(/unlink/||/rename/||/unlinkat/||/renameat/) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F arch=b64 -S unlink,unlinkat,rename,renameat -F auid>=1000 -
F auid!=unset -k delete
-a always,exit -F arch=b32 -S unlink,unlinkat,rename,renameat -F auid>=1000 -
F auid!=unset -k delete
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -S/ \
&&(/unlink/||/rename/||/unlinkat/||/renameat/) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F arch=b64 -S rename,unlink,unlinkat,renameat -F auid>=1000 -
F auid!=-1 -F key=delete
-a always,exit -F arch=b32 -S unlink,rename,unlinkat,renameat -F auid>=1000 -
F auid!=-1 -F key=delete
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor file deletion events by users.
Example:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F arch=b64 -S rename,unlink,unlinkat,renameat -F
auid>=${UID_MIN} -F auid!=unset -F key=delete
-a always,exit -F arch=b32 -S rename,unlink,unlinkat,renameat -F
auid>=${UID_MIN} -F auid!=unset -F key=delete
" >> /etc/audit/rules.d/50-delete.rules || printf "ERROR: Variable 'UID_MIN'
is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.14 — 6.2.3.14 Ensure events that modify the system's Mandatory (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor AppArmor, an implementation of mandatory access controls. The parameters
below monitor any write access (potential additional, deletion or modification of files in
the directory) or attribute changes to the /etc/apparmor/ and /etc/apparmor.d/
directories.
Note: If a different Mandatory Access Control method is used, changes to the
corresponding directories should be audited.

**Rationale**:

Changes to files in the /etc/apparmor/ and /etc/apparmor.d/ directories could
indicate that an unauthorized user is attempting to modify access controls and change
security contexts, leading to a compromise of the system.

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# awk '/^ *-w/ \
&&(/\/etc\/apparmor/ \
||/\/etc\/apparmor.d/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-w /etc/apparmor/ -p wa -k MAC-policy
-w /etc/apparmor.d/ -p wa -k MAC-policy
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk '/^ *-w/ \
&&(/\/etc\/apparmor/ \
||/\/etc\/apparmor.d/) \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
Verify the output matches:
-w /etc/apparmor/ -p wa -k MAC-policy
-w /etc/apparmor.d/ -p wa -k MAC-policy
```

**Remediation**:

```
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor events that modify the system's Mandatory Access
Controls.
Example:
# printf "
-w /etc/apparmor/ -p wa -k MAC-policy
-w /etc/apparmor.d/ -p wa -k MAC-policy
" >> /etc/audit/rules.d/50-MAC-policy.rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.15 — 6.2.3.15 Ensure successful and unsuccessful attempts to use the (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

The operating system must generate audit records for successful/unsuccessful uses of
the chcon command.

**Rationale**:

The chcon command is used to change file security context. Without generating audit
records that are specific to the security and mission needs of the organization, it would
be difficult to establish, correlate, and investigate the events relating to an incident or
identify those responsible for one.
Audit records can be generated from various components within the information system
(e.g., module or policy filter).

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/bin\/chcon/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F path=/usr/bin/chcon -F perm=x -F auid>=1000 -F auid!=unset
-k perm_chng
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/bin\/chcon/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -S all -F path=/usr/bin/chcon -F perm=x -F auid>=1000 -F
auid!=-1 -F key=perm_chng
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor successful and unsuccessful attempts to use the
chcon command.
Example:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F path=/usr/bin/chcon -F perm=x -F auid>=${UID_MIN} -F
auid!=unset -k perm_chng
" >> /etc/audit/rules.d/50-perm_chng.rules || printf "ERROR: Variable
'UID_MIN' is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.16 — 6.2.3.16 Ensure successful and unsuccessful attempts to use the (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

The operating system must generate audit records for successful/unsuccessful uses of
the setfacl command

**Rationale**:

This utility sets Access Control Lists (ACLs) of files and directories. Without generating
audit records that are specific to the security and mission needs of the organization, it
would be difficult to establish, correlate, and investigate the events relating to an
incident or identify those responsible for one.
Audit records can be generated from various components within the information system
(e.g., module or policy filter).

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/bin\/setfacl/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules ||
printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F path=/usr/bin/setfacl -F perm=x -F auid>=1000 -F
auid!=unset -k perm_chng
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/bin\/setfacl/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -S all -F path=/usr/bin/setfacl -F perm=x -F auid>=1000 -F
auid!=-1 -F key=perm_chng
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor successful and unsuccessful attempts to use the
setfacl command.
Example:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F path=/usr/bin/setfacl -F perm=x -F auid>=${UID_MIN} -F
auid!=unset -k perm_chng
" >> /etc/audit/rules.d/50-perm_chng.rules || printf "ERROR: Variable
'UID_MIN' is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.17 — 6.2.3.17 Ensure successful and unsuccessful attempts to use the (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

The operating system must generate audit records for successful/unsuccessful uses of
the chacl command.
chacl is an IRIX-compatibility command, and is maintained for those users who are
familiar with its use from either XFS or IRIX.

**Rationale**:

chacl changes the ACL(s) for a file or directory. Without generating audit records that
are specific to the security and mission needs of the organization, it would be difficult to
establish, correlate, and investigate the events relating to an incident or identify those
responsible for one.
Audit records can be generated from various components within the information system
(e.g., module or policy filter).

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/bin\/chacl/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F path=/usr/bin/chacl -F perm=x -F auid>=1000 -F auid!=unset
-k perm_chng
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/bin\/chacl/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -S all -F path=/usr/bin/chacl -F perm=x -F auid>=1000 -F
auid!=-1 -F key=perm_chng
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor successful and unsuccessful attempts to use the
chacl command.
Example:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F path=/usr/bin/chacl -F perm=x -F auid>=${UID_MIN} -F
auid!=unset -k perm_chng
" >> /etc/audit/rules.d/50-perm_chng.rules || printf "ERROR: Variable
'UID_MIN' is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.18 — 6.2.3.18 Ensure successful and unsuccessful attempts to use the (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

The operating system must generate audit records for successful/unsuccessful uses of
the usermod command.

**Rationale**:

The usermod command modifies the system account files to reflect the changes that are
specified on the command line. Without generating audit records that are specific to the
security and mission needs of the organization, it would be difficult to establish,
correlate, and investigate the events relating to an incident or identify those responsible
for one.
Audit records can be generated from various components within the information system
(e.g., module or policy filter).

**Audit guide**:

```
On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/sbin\/usermod/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F path=/usr/sbin/usermod -F perm=x -F auid>=1000 -F
auid!=unset -k usermod
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/sbin\/usermod/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -S all -F path=/usr/sbin/usermod -F perm=x -F auid>=1000 -F
auid!=-1 -F key=usermod
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor successful and unsuccessful attempts to use the
usermod command.
Example:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F path=/usr/sbin/usermod -F perm=x -F auid>=${UID_MIN} -F
auid!=unset -k usermod
" >> /etc/audit/rules.d/50-usermod.rules || printf "ERROR: Variable 'UID_MIN'
is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 6.2.3.19 — 6.2.3.19 Ensure kernel module loading unloading and (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 2 - Server, Level 2 - Workstation

**Description**:

Monitor the loading and unloading of kernel modules. All the loading / listing /
dependency checking of modules is done by kmod via symbolic links.
The following system calls control loading and unloading of modules:
• init_module - load a module
• finit_module - load a module (used when the overhead of using
cryptographically signed modules to determine the authenticity of a module can
be avoided)
• delete_module - delete a module
• create_module - create a loadable module entry
• query_module - query the kernel for various bits pertaining to modules
Any execution of the loading and unloading module programs and system calls will
trigger an audit record with an identifier of modules.

**Rationale**:

Monitoring the use of all the various ways to manipulate kernel modules could provide
system administrators with evidence that an unauthorized change was made to a kernel
module, possibly compromising the security of the system.

**Audit guide**:

```
On disk configuration
Run the following script to check the on disk rules:
#!/usr/bin/env bash
{
awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F auid!=unset/||/ -F auid!=-1/||/ -F auid!=4294967295/) \
&&/ -S/ \
&&(/init_module/ \
||/finit_module/ \
||/delete_module/ \
||/create_module/ \
||/query_module/) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/bin\/kmod/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output matches:
-a always,exit -F arch=b64 -S
init_module,finit_module,delete_module,create_module,query_module -F
auid>=1000 -F auid!=unset -k kernel_modules
-a always,exit -F path=/usr/bin/kmod -F perm=x -F auid>=1000 -F auid!=unset -
k kernel_modules
Running configuration
Run the following script to check loaded rules:
#!/usr/bin/env bash
{
auditctl -l | awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&(/ -F auid!=unset/||/ -F auid!=-1/||/ -F auid!=4294967295/) \
&&/ -S/ \
&&(/init_module/ \
||/finit_module/ \
||/delete_module/ \
||/create_module/ \
||/query_module/) \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/ \
&&(/ -F *auid!=unset/||/ -F *auid!=-1/||/ -F *auid!=4294967295/) \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -F *perm=x/ \
&&/ -F *path=\/usr\/bin\/kmod/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)" \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output includes:
-a always,exit -F arch=b64 -S
create_module,init_module,delete_module,query_module,finit_module -F
auid>=1000 -F auid!=-1 -F key=kernel_modules
-a always,exit -S all -F path=/usr/bin/kmod -F perm=x -F auid>=1000 -F
auid!=-1 -F key=kernel_modules
Symlink audit
Run the following script to audit if the symlinks kmod accepts are indeed pointing at it:
#!/usr/bin/env bash
{
a_files=("/usr/sbin/lsmod" "/usr/sbin/rmmod" "/usr/sbin/insmod"
"/usr/sbin/modinfo" "/usr/sbin/modprobe" "/usr/sbin/depmod")
for l_file in "${a_files[@]}"; do
if [ "$(readlink -f "$l_file")" = "$(readlink -f /bin/kmod)" ]; then
printf "OK: \"$l_file\"\n"
else
printf "Issue with symlink for file: \"$l_file\"\n"
fi
done
}
Verify the output states OK. If there is a symlink pointing to a different location it should
be investigated
```

**Remediation**:

```
Create audit rules
Edit or create a file in the /etc/audit/rules.d/ directory, ending in .rules extension,
with the relevant rules to monitor kernel module modification.
Example:
#!/usr/bin/env bash
{
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && printf "
-a always,exit -F arch=b64 -S
init_module,finit_module,delete_module,create_module,query_module -F
auid>=${UID_MIN} -F auid!=unset -k kernel_modules
-a always,exit -F path=/usr/bin/kmod -F perm=x -F auid>=${UID_MIN} -F
auid!=unset -k kernel_modules
" >> /etc/audit/rules.d/50-kernel_modules.rules || printf "ERROR: Variable
'UID_MIN' is unset.\n"
}
Load audit rules
Merge and load the rules into active configuration:
# augenrules --load
Check if reboot is required.
# if [[ $(auditctl -s | grep "enabled") =~ "2" ]]; then printf "Reboot
required to load rules\n"; fi
```

---

### 7.1.10 — 7.1.10 Ensure permissions on /etc/security/opasswd are (Automated)

**Reason**: `audit lacks ** PASS ** marker (natural-language manual)`

**Assessment**: Automated

**Profile**: Level 1 - Server, Level 1 - Workstation

**Description**:

/etc/security/opasswd and it's backup /etc/security/opasswd.old hold user's
previous passwords if pam_unix or pam_pwhistory is in use on the system

**Rationale**:

It is critical to ensure that /etc/security/opasswd is protected from unauthorized
access. Although it is protected by default, the file permissions could be changed either
inadvertently or through malicious actions.

**Audit guide**:

```
Run the following commands to verify /etc/security/opasswd and
/etc/security/opasswd.old are mode 600 or more restrictive, Uid is 0/root and
Gid is 0/root if they exist:
# [ -e "/etc/security/opasswd" ] && stat -Lc '%n Access: (%#a/%A) Uid: (
%u/ %U) Gid: ( %g/ %G)' /etc/security/opasswd
/etc/security/opasswd Access: (0600/-rw-------) Uid: ( 0/ root) Gid: ( 0/
root)
-OR-
Nothing is returned
# [ -e "/etc/security/opasswd.old" ] && stat -Lc '%n Access: (%#a/%A) Uid:
( %u/ %U) Gid: ( %g/ %G)' /etc/security/opasswd.old
/etc/security/opasswd.old Access: (0600/-rw-------) Uid: ( 0/ root) Gid: (
0/ root)
-OR-
Nothing is returned
```

**Remediation**:

```
Run the following commands to remove excess permissions, set owner, and set group
on /etc/security/opasswd and /etc/security/opasswd.old is they exist:
# [ -e "/etc/security/opasswd" ] && chmod u-x,go-rwx /etc/security/opasswd
# [ -e "/etc/security/opasswd" ] && chown root:root /etc/security/opasswd
# [ -e "/etc/security/opasswd.old" ] && chmod u-x,go-rwx
/etc/security/opasswd.old
# [ -e "/etc/security/opasswd.old" ] && chown root:root
/etc/security/opasswd.old
Default Value:
/etc/security/opasswd Access: (0600/-rw-------) Uid: ( 0/ root) Gid: ( 0/ root)
```

---

