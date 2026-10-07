/* Old Docker seccomp profiles (Docker 20.10 + libseccomp 2.5.1 on this host)
 * answer close_range(2) with EPERM instead of ENOSYS. GLib >= 2.74 then refuses
 * to spawn any child ("Failed to close file descriptor for child process"),
 * which breaks xfce4-session, Thunar, the panel launcher and most GTK apps.
 * Turn that EPERM into ENOSYS so GLib and glibc use their /proc/self/fd
 * fallback. Hosts where close_range works are not affected. */
#define _GNU_SOURCE
#include <errno.h>
#include <unistd.h>
#include <sys/syscall.h>

#ifndef SYS_close_range
#define SYS_close_range 436
#endif

int close_range(unsigned int first, unsigned int last, int flags)
{
    long r = syscall(SYS_close_range, first, last, flags);
    if (r == -1 && errno == EPERM)
        errno = ENOSYS;
    return (int)r;
}
