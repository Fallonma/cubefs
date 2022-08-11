#ifndef CLIENT_H
#define CLIENT_H

#include <dirent.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <gnu/libc-version.h>
#include <limits.h>
#include <pthread.h>
#include <search.h>
#include <signal.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/types.h>
#include <time.h>
#include <unistd.h>
#include <utime.h>
#include <map>
#include <set>
#include "cache.h"
#include "conn_pool.h"
#include "ini.h"
#include "packet.h"
#include "sdk.h"
#include "util.h"
#include "cache.h"

using namespace std;

// Define ALIASNAME as a weak alias for NAME.
# define weak_alias(name, aliasname) extern __typeof (name) aliasname __attribute__ ((weak, alias (#name)));

// compatible for glibc before 2.18
#ifndef RENAME_NOREPLACE
#define RENAME_NOREPLACE (1 << 0)
#endif

/*
 * The implementation of opendir depend on struct __dirstream
 */
#define __libc_lock_define(CLASS,NAME)
struct __dirstream
{
    int fd;			/* File descriptor.  */

    __libc_lock_define (, lock) /* Mutex lock for this structure.  */

    size_t allocation;		/* Space allocated for the block.  */
    size_t size;		/* Total valid data in the block.  */
    size_t offset;		/* Current offset into the block.  */

    off_t filepos;		/* Position of next entry to read.  */

    int errcode;		/* Delayed error code.  */

    /* Directory block.  We must make sure that this block starts
       at an address that is aligned adequately enough to store
       dirent entries.  Using the alignment of "void *" is not
       sufficient because dirents on 32-bit platforms can require
       64-bit alignment.  We use "long double" here to be consistent
       with what malloc uses.  */
    char data[0] __attribute__ ((aligned (__alignof__ (long double))));
};

#ifdef __cplusplus
extern "C"
{
#endif

int real_close(int fd);
int real_openat(int dirfd, const char *pathname, int flags, ...);
int real_renameat2(int olddirfd, const char *old_pathname,
        int newdirfd, const char *new_pathname, unsigned int flags);
int real_truncate(const char *pathname, off_t length);
int real_ftruncate(int fd, off_t length);
int real_fallocate(int fd, int mode, off_t offset, off_t len);
int real_posix_fallocate(int fd, off_t offset, off_t len);
int real_mkdirat(int dirfd, const char *pathname, mode_t mode);
int real_rmdir(const char *pathname);
char *real_getcwd(char *buf, size_t size);
int real_chdir(const char *pathname);
int real_fchdir(int fd);
DIR *real_opendir(const char *pathname);
DIR *real_fdopendir(int fd);
struct dirent *real_readdir(DIR *dirp);
int real_closedir(DIR *dirp);
char *real_realpath(const char *path, char *resolved_path);
int real_linkat(int olddirfd, const char *old_pathname,
           int newdirfd, const char *new_pathname, int flags);
int real_symlinkat(const char *target, int dirfd, const char *linkpath);
int real_unlinkat(int dirfd, const char *pathname, int flags);
ssize_t real_readlinkat(int dirfd, const char *pathname, char *buf, size_t size);
int real_stat(int ver, const char *pathname, struct stat *statbuf);
int real_stat64(int ver, const char *pathname, struct stat64 *statbuf);
int real_lstat(int ver, const char *pathname, struct stat *statbuf);
int real_lstat64(int ver, const char *pathname, struct stat64 *statbuf);
int real_fstat(int ver, int fd, struct stat *statbuf);
int real_fstat64(int ver, int fd, struct stat64 *statbuf);
int real_fstatat(int ver, int dirfd, const char *pathname, struct stat *statbuf, int flags);
int real_fstatat64(int ver, int dirfd, const char *pathname, struct stat64 *statbuf, int flags);
int real_fchmod(int fd, mode_t mode);
int real_fchmodat(int dirfd, const char *pathname, mode_t mode, int flags);
int real_lchown(const char *pathname, uid_t owner, gid_t group);
int real_fchown(int fd, uid_t owner, gid_t group);
int real_fchownat(int dirfd, const char *pathname, uid_t owner, gid_t group, int flags);
int real_utime(const char *pathname, const struct utimbuf *times);
int real_utimes(const char *pathname, const struct timeval *times);
int real_futimesat(int dirfd, const char *pathname, const struct timeval times[2]);
int real_utimensat(int dirfd, const char *pathname, const struct timespec times[2], int flags);
int real_futimens(int fd, const struct timespec times[2]);
int real_faccessat(int dirfd, const char *pathname, int mode, int flags);
int real_setxattr(const char *pathname, const char *name,
        const void *value, size_t size, int flags);
int real_lsetxattr(const char *pathname, const char *name,
        const void *value, size_t size, int flags);
int real_fsetxattr(int fd, const char *name, const void *value, size_t size, int flags);
ssize_t real_getxattr(const char *pathname, const char *name, void *value, size_t size);
ssize_t real_lgetxattr(const char *pathname, const char *name, void *value, size_t size);
ssize_t real_fgetxattr(int fd, const char *name, void *value, size_t size);
ssize_t real_listxattr(const char *pathname, char *list, size_t size);
ssize_t real_llistxattr(const char *pathname, char *list, size_t size);
ssize_t real_flistxattr(int fd, char *list, size_t size);
int real_removexattr(const char *pathname, const char *name);
int real_lremovexattr(const char *pathname, const char *name);
int real_fremovexattr(int fd, const char *name);
int real_fcntl(int fd, int cmd, ...);
int real_dup2(int oldfd, int newfd);
int real_dup3(int oldfd, int newfd, int flags);
ssize_t real_read(int fd, void *buf, size_t count);
ssize_t real_readv(int fd, const struct iovec *iov, int iovcnt);
ssize_t real_pread(int fd, void *buf, size_t count, off_t offset);
ssize_t real_preadv(int fd, const struct iovec *iov, int iovcnt, off_t offset);
ssize_t real_write(int fd, const void *buf, size_t count);
ssize_t real_writev(int fd, const struct iovec *iov, int iovcnt);
ssize_t real_pwrite(int fd, const void *buf, size_t count, off_t offset);
ssize_t real_pwritev(int fd, const struct iovec *iov, int iovcnt, off_t offset);
off_t real_lseek(int fd, off_t offset, int whence);
int real_fdatasync(int fd);
int real_fsync(int fd);

int start_libs(void*);
void* stop_libs();
void flush_logs();
#ifdef __cplusplus
}
#endif

typedef int (*openat_t)(int dirfd, const char *pathname, int flags, mode_t mode);
typedef int (*close_t)(int fd);
typedef int (*renameat_t)(int olddirfd, const char *oldpath, int newdirfd, const char *newpath);
typedef int (*renameat2_t)(int olddirfd, const char *oldpath, int newdirfd, const char *newpath, unsigned int flags);
typedef int (*truncate_t)(const char *path, off_t length);
typedef int (*ftruncate_t)(int fd, off_t length);
typedef int (*fallocate_t)(int fd, int mode, off_t offset, off_t len);
typedef int (*posix_fallocate_t)(int fd, off_t offset, off_t len);

typedef int (*chdir_t)(const char *path);
typedef int (*fchdir_t)(int fd);
typedef char *(*getcwd_t)(char *buf, size_t size);
typedef int (*mkdirat_t)(int dirfd, const char *pathname, mode_t mode);
typedef int (*rmdir_t)(const char *pathname);
typedef DIR *(*opendir_t)(const char *name);
typedef DIR *(*fdopendir_t)(int fd);
typedef struct dirent *(*readdir_t)(DIR *dirp);
typedef int (*closedir_t)(DIR *dirp);
typedef char *(*realpath_t)(const char *path, char *resolved_path);

typedef int (*linkat_t)(int olddirfd, const char *oldpath, int newdirfd, const char *newpath, int flags);
typedef int (*symlinkat_t)(const char *target, int newdirfd, const char *linkpath);
typedef int (*unlinkat_t)(int dirfd, const char *pathname, int flags);
typedef ssize_t (*readlinkat_t)(int dirfd, const char *pathname, char *buf, size_t size);

typedef int (*stat_t)(int ver, const char *pathname, struct stat *statbuf);
typedef int (*stat64_t)(int ver, const char *pathname, struct stat64 *statbuf);
typedef int (*lstat_t)(int ver, const char *pathname, struct stat *statbuf);
typedef int (*lstat64_t)(int ver, const char *pathname, struct stat64 *statbuf);
typedef int (*fstat_t)(int ver, int fd, struct stat *statbuf);
typedef int (*fstat64_t)(int ver, int fd, struct stat64 *statbuf);
typedef int (*fstatat_t)(int ver, int dirfd, const char *pathname, struct stat *statbuf, int flags);
typedef int (*fstatat64_t)(int ver, int dirfd, const char *pathname, struct stat64 *statbuf, int flags);
typedef int (*fchmod_t)(int fd, mode_t mode);
typedef int (*fchmodat_t)(int dirfd, const char *pathname, mode_t mode, int flags);
typedef int (*lchown_t)(const char *pathname, uid_t owner, gid_t group);
typedef int (*fchown_t)(int fd, uid_t owner, gid_t group);
typedef int (*fchownat_t)(int dirfd, const char *pathname, uid_t owner, gid_t group, int flags);
typedef int (*utime_t)(const char *filename, const struct utimbuf *times);
typedef int (*utimes_t)(const char *filename, const struct timeval times[2]);
typedef int (*futimesat_t)(int dirfd, const char *pathname, const struct timeval times[2]);
typedef int (*utimensat_t)(int dirfd, const char *pathname, const struct timespec times[2], int flags);
typedef int (*futimens_t)(int fd, const struct timespec times[2]);
typedef int (*access_t)(const char *pathname, int mode);
typedef int (*faccessat_t)(int dirfd, const char *pathname, int mode, int flags);

typedef int (*setxattr_t)(const char *path, const char *name, const void *value, size_t size, int flags);
typedef int (*lsetxattr_t)(const char *path, const char *name, const void *value, size_t size, int flags);
typedef int (*fsetxattr_t)(int fd, const char *name, const void *value, size_t size, int flags);
typedef ssize_t (*getxattr_t)(const char *path, const char *name, void *value, size_t size);
typedef ssize_t (*lgetxattr_t)(const char *path, const char *name, void *value, size_t size);
typedef ssize_t (*fgetxattr_t)(int fd, const char *name, void *value, size_t size);
typedef ssize_t (*listxattr_t)(const char *path, char *list, size_t size);
typedef ssize_t (*llistxattr_t)(const char *path, char *list, size_t size);
typedef ssize_t (*flistxattr_t)(int fd, char *list, size_t size);
typedef int (*removexattr_t)(const char *path, const char *name);
typedef int (*lremovexattr_t)(const char *path, const char *name);
typedef int (*fremovexattr_t)(int fd, const char *name);

typedef int (*fcntl_t)(int fd, int cmd, ...);
typedef int (*dup2_t)(int oldfd, int newfd);
typedef int (*dup3_t)(int oldfd, int newfd, int flags);

typedef ssize_t (*read_t)(int fd, void *buf, size_t count);
typedef ssize_t (*readv_t)(int fd, const struct iovec *iov, int iovcnt);
typedef ssize_t (*pread_t)(int fd, void *buf, size_t count, off_t offset);
typedef ssize_t (*preadv_t)(int fd, const struct iovec *iov, int iovcnt, off_t offset);
typedef ssize_t (*write_t)(int fd, const void *buf, size_t count);
typedef ssize_t (*writev_t)(int fd, const struct iovec *iov, int iovcnt);
typedef ssize_t (*pwrite_t)(int fd, const void *buf, size_t count, off_t offset);
typedef ssize_t (*pwritev_t)(int fd, const struct iovec *iov, int iovcnt, off_t offset);
typedef off_t (*lseek_t)(int fd, off_t offset, int whence);
typedef off64_t (*lseek64_t)(int fd, off64_t offset, int whence);

typedef int (*fdatasync_t)(int fd);
typedef int (*fsync_t)(int fd);

//typedef int (*sigaction_t)(int signum, const struct sigaction *act, struct sigaction *oldact);

static openat_t libc_openat;
static close_t libc_close;
static renameat_t libc_renameat;
static renameat2_t libc_renameat2;
static truncate_t libc_truncate;
static ftruncate_t libc_ftruncate;
static fallocate_t libc_fallocate;
static posix_fallocate_t libc_posix_fallocate;

static chdir_t libc_chdir;
static fchdir_t libc_fchdir;
static getcwd_t libc_getcwd;
static mkdirat_t libc_mkdirat;
static rmdir_t libc_rmdir;
static opendir_t libc_opendir;
static fdopendir_t libc_fdopendir;
static readdir_t libc_readdir;
static closedir_t libc_closedir;
static realpath_t libc_realpath;

static linkat_t libc_linkat;
static symlinkat_t libc_symlinkat;
static unlinkat_t libc_unlinkat;
static readlinkat_t libc_readlinkat;

static stat_t libc_stat;
static stat64_t libc_stat64;
static lstat_t libc_lstat;
static lstat64_t libc_lstat64;
static fstat_t libc_fstat;
static fstat64_t libc_fstat64;
static fstatat_t libc_fstatat;
static fstatat64_t libc_fstatat64;
static fchmod_t libc_fchmod;
static fchmodat_t libc_fchmodat;
static lchown_t libc_lchown;
static fchown_t libc_fchown;
static fchownat_t libc_fchownat;
static utime_t libc_utime;
static utimes_t libc_utimes;
static futimesat_t libc_futimesat;
static utimensat_t libc_utimensat;
static futimens_t libc_futimens;
static access_t libc_access;
static faccessat_t libc_faccessat;

static setxattr_t libc_setxattr;
static lsetxattr_t libc_lsetxattr;
static fsetxattr_t libc_fsetxattr;
static getxattr_t libc_getxattr;
static lgetxattr_t libc_lgetxattr;
static fgetxattr_t libc_fgetxattr;
static listxattr_t libc_listxattr;
static llistxattr_t libc_llistxattr;
static flistxattr_t libc_flistxattr;
static removexattr_t libc_removexattr;
static lremovexattr_t libc_lremovexattr;
static fremovexattr_t libc_fremovexattr;

static fcntl_t libc_fcntl;
static dup2_t libc_dup2;
static dup3_t libc_dup3;

static read_t libc_read;
static readv_t libc_readv;
static pread_t libc_pread;
static preadv_t libc_preadv;
static write_t libc_write;
static writev_t libc_writev;
static pwrite_t libc_pwrite;
static pwritev_t libc_pwritev;
static lseek_t libc_lseek;
static lseek64_t libc_lseek64;

static fdatasync_t libc_fdatasync;
static fsync_t libc_fsync;

//static sigaction_t libc_sigaction;

#define CFS_FD_MASK (1 << (sizeof(int)*8 - 2))

static const char *CFS_CFG_PATH = "cfs_client.ini";
static const char *CFS_CFG_PATH_JED = "/export/servers/cfs/cfs_client.ini";
static const uint8_t FILE_TYPE_BIN_LOG = 1;
static const uint8_t FILE_TYPE_REDO_LOG = 2;
static const uint8_t FILE_TYPE_RELAY_LOG = 3;
static const char *BIN_LOG_PREFIX = "mysql-bin.";
static const char *REDO_LOG_PREFIX = "ib_logfile";
static const char *RELAY_LOG_PREFIX = "relay-bin.";

// hook or not, currently for test
const bool g_hook = true;

typedef struct {
    int fd;
    int flags;
    off_t pos;
    int dup_ref;
    int file_type;
    inode_info_t *inode_info;
} file_t;

typedef struct {
    char *sdk_state;
    cfs_file_t *files;
    int file_num;
    int* dup_fds;
    int fd_num;
    char *cwd;
    bool in_cfs;
} client_state_t;

typedef struct {
     char* mount_point;
     char* ignore_path;
     char* log_dir;
     char* log_level;
     char* prof_port;
} client_config_t;

typedef struct {
    map<int, int> dup_fds;
    pthread_rwlock_t open_files_lock;
    map<int, file_t *> open_files;
    pthread_rwlock_t open_inodes_lock;
    map<ino_t, inode_info_t *> open_inodes;

    lru_cache_t *big_page_cache;
    lru_cache_t *small_page_cache;
    conn_pool_t *conn_pool;

    // map for each open fd to its pathname, to print pathname in debug log
    map<int, char *> fd_path;
    pthread_rwlock_t fd_path_lock;

    // the current working directory, doesn't include the mount point part if in cfs
    char *cwd;
    // whether the _cwd is in CFS or not
    bool in_cfs;
    int64_t cfs_client_id;
    bool has_renameat2;

    const char *mount_point;
    const char *ignore_path;
    const char *config_path;
    pthread_t bg_pthread;
    void* sdk_handle;
    bool stop;
    inode_wrapper_t inode_wrapper;
} client_info_t;

static client_info_t g_client_info;

static void init();
static void init_libc_func();
static void init_cfs_func(void *);
static void *plugin_open(const char*);
static int plugin_close(void*);
static int record_open_file(cfs_file_t *);

static file_t *get_open_file(int fd);
static bool try_get_cfs_fd(int *fd_ptr);
static bool try_get_dupped_fd(int *fd_ptr);
static int config_handler(void* user, const char* section,
        const char* name, const char* value) {
    client_config_t *pconfig = (client_config_t*)user;
    #define MATCH(s, n) strcmp(section, s) == 0 && strcmp(name, n) == 0

    if (MATCH("", "mountPoint")) {
        pconfig->mount_point = strdup(value);
    } else if (MATCH("", "ignorePath")) {
        pconfig->ignore_path = strdup(value);
    } else if (MATCH("", "logDir")) {
        pconfig->log_dir = strdup(value);
    } else if (MATCH("", "logLevel")) {
        pconfig->log_level = strdup(value);
    } else if (MATCH("", "profPort")) {
        pconfig->prof_port = strdup(value);
    } else {
        return 0;  /* unknown section/name, error */
    }
    return 1;
}

/*
 * get_clean_path is a c implementation of golang path.Clean().
 * The caller should free the returned buffer.
 *
 * Function returns the shortest path name equivalent to path
 * by purely lexical processing. It applies the following rules
 * iteratively until no further processing can be done:
 *
 *	1. Replace multiple slashes with a single slash.
 *	2. Eliminate each . path name element (the current directory).
 *	3. Eliminate each inner .. path name element (the parent directory)
 *	   along with the non-.. element that precedes it.
 *	4. Eliminate .. elements that begin a rooted path:
 *	   that is, replace "/.." by "/" at the beginning of a path.
 *
 * The returned path ends in a slash only if it is the root "/".
 *
 * If the result of this process is an empty string, function returns the string ".".
 */
static char *get_clean_path(const char *path) {
    if(path == NULL) {
        return NULL;
    }

    int rooted = path[0] == '/';
    int n = strlen(path);

    // Invariants:
    //	reading from path; r is index of next byte to process.
    //	writing to buf; w is index of next byte to write.
    //	dotdot is index in buf where .. must stop, either because
    //		it is the leading slash or it is a leading ../../.. prefix.
    char *out = (char *) malloc(n + 1);
    if(out == NULL) {
        return NULL;
    }
    int r = 0, w = 0, dotdot = 0;
    if(rooted) {
        out[w++] = '/';
        r = 1, dotdot = 1;
    }

    while(r < n) {
        if(path[r] == '/') {
            // empty path element
            r++;
        } else if(path[r] == '.' && (r + 1 == n || path[r + 1] == '/')) {
            // . element
            r++;
        } else if(path[r] == '.' && path[r + 1] == '.' && (r + 2 == n || path[r + 2] == '/')) {
            // .. element: remove to last /
            r += 2;
            if(w > dotdot) {
                // can backtrack
                w--;
                while(w > dotdot && out[w] != '/') {
                    w--;
                }
            } else if(!rooted) {
                // cannot backtrack, but not rooted, so append .. element.
                if(w > 0) {
                    out[w++] = '/';
                }

                out[w++] = '.';
                out[w++] = '.';
                dotdot = w;
            }
        } else {
            // real path element.
            // add slash if needed
            if(rooted && w != 1 || !rooted && w != 0) {
                out[w++] = '/';
            }
            // copy element
            for(; r < n && path[r] != '/'; r++) {
                out[w++] = path[r];
            }
        }
    }

    // Turn empty string into "."
    if(w == 0) {
        out[w++] = '.';
    }
    out[w] = '\0';
    return out;
}

/*
 * cat_path concatenate the cwd and the relative path.
 * The caller should free the returned buffer.
 */
static char *cat_path(const char *cwd, const char *pathname) {
    if(cwd == NULL || pathname == NULL) {
        return NULL;
    }

    int len = strlen(cwd) + strlen(pathname) + 2;
    char *path = (char *)malloc(len);
    if(path == NULL) {
        return NULL;
    }

    memset(path, '\0', len);
    strcat(path, cwd);
    strcat(path, "/");
    strcat(path, pathname);
    return path;
}

/*
 * Return the remainder part if input path is in CFS, stripping the mount point part.
 * The mount point part MUST be stripped before passing to CFS.
 * Return NULL if input path is not in CFS or an error occured.
 * The caller should free the returned buffer.
 */
static char *get_cfs_path(const char *pathname) {
    if(pathname == NULL || (pathname[0] != '/' && !g_client_info.in_cfs)) {
        return NULL;
    }

    // realpath() in glibc cannot be used here.
    // There are two reasons:
    // 1. realpath() depends on _lxstat64(), which in turn depends on get_cfs_path().
    //    This causes circular dependencies.
    // 2. realpath() uses _lxstat64() many times to validate directory,
    //    which is needless and harm the performance.
    char *real_path = get_clean_path(pathname);
    if(real_path == NULL) {
        return NULL;
    }

    char *result;
    if(pathname[0] != '/' && g_client_info.in_cfs) {
        result = cat_path(g_client_info.cwd, real_path);
        free(real_path);
        return result;
    }

    // check if real_path contains mount_point, and doesn't contain ignore_path
    // the mount_point has been strip off the last '/' in cfs_init()
    size_t len = strlen(g_client_info.mount_point);
    size_t len_real = strlen(real_path);
    bool is_cfs = false;
    char *ignore_path = strdup(g_client_info.ignore_path);
    if(ignore_path == NULL) {
        free(real_path);
        return NULL;
    }
    if(strncmp(real_path, g_client_info.mount_point, len) == 0) {
        if(strlen(g_client_info.ignore_path) > 0) {
            char *token = strtok(ignore_path, ",");
            size_t len_token;
            while(token != NULL) {
                len_token = strlen(token);
                if(real_path[len] == '/' && strncmp(real_path+len+1, token, len_token) == 0 &&
                (real_path[len+1+len_token] == '\0' || real_path[len+1+len_token] == '/')) {
                    is_cfs = false;
                    break;
                }
                is_cfs = true;
                token = strtok(NULL, ",");
            }
        } else if(real_path[len] == '\0' || real_path[len] == '/') {
            is_cfs = true;
        }
    }
    free(ignore_path);

    if (!is_cfs) {
        free(real_path);
        return NULL;
    }

    // strip the mount point part for path in CFS
    int len_result = len_real - len;
    result = (char *) malloc((len_result == 0 ? 1 : len_result) + 1);
    if (result == NULL) {
        free(real_path);
        return NULL;
    }
    if (len_result > 0) {
        memcpy(result, real_path + len, len_result);
    } else {
        result[0] = '/';
    }
    result[len_result == 0 ? 1 : len_result] = '\0';
    free(real_path);
    return result;
}

// process returned int from cfs functions
static int cfs_errno(int re) {
    if(re < 0) {
        errno = -re;
        re = -1;
    } else {
        errno = 0;
    }
    return re;
}

// process returned ssize_t from cfs functions
static ssize_t cfs_errno_ssize_t(ssize_t re) {
    if(re < 0) {
        errno = -re;
        re = -1;
    } else {
        errno = 0;
    }
    return re;
}

/*
static void signal_handler(int signum) {
    cfs_flush_log();
    if(g_sa_handler[signum] && g_sa_handler[signum] != SIG_IGN && g_sa_handler[signum] != SIG_DFL) {
        g_sa_handler[signum](signum);
    }
    #ifdef _CFS_DEBUG
    printf("%s, signum:%d\n", __func__, signum);
    #endif
}
*/

static bool has_renameat2() {
    const char *ver = gnu_get_libc_version();
    char *ver1 = strdup(ver);
    if(ver1 == NULL) {
        return false;
    }
    char *delimiter = strstr(ver1, ".");
    int len = 0;
    if(delimiter != NULL) {
        len = strlen(delimiter);
        delimiter[0] = '\0';
    }
    int major = atoi(ver1);
    int minor = 0;
    if(len > 1) {
        minor = atoi(delimiter + 1);
    }
    free(ver1);
    return major > 2 || (major == 2 && minor >= 28);
}

bool fd_in_cfs(int fd) {
    if(g_client_info.dup_fds.find(fd) != g_client_info.dup_fds.end()) {
        return true;
    }

    if (fd & CFS_FD_MASK) {
        return true;
    }
    return false;
}

int get_cfs_fd(int fd) {
    int cfs_fd = -1;
    auto it = g_client_info.dup_fds.find(fd);
    if(it != g_client_info.dup_fds.end()) {
        cfs_fd = it->second;
    } else if (fd & CFS_FD_MASK) {
        cfs_fd = fd & ~CFS_FD_MASK;
    }
    return cfs_fd;
}

int dup_fd(int oldfd, int newfd) {
    pthread_rwlock_rdlock(&g_client_info.open_files_lock);
    auto it = g_client_info.open_files.find(oldfd);
    if (it == g_client_info.open_files.end()) {
        pthread_rwlock_unlock(&g_client_info.open_files_lock);
        return -1;
    }
    it->second->dup_ref++;
    pthread_rwlock_unlock(&g_client_info.open_files_lock);
    g_client_info.dup_fds[newfd] = oldfd;
    return newfd;
}

file_t *get_open_file(int fd) {
    pthread_rwlock_rdlock(&g_client_info.open_files_lock);
    auto it = g_client_info.open_files.find(fd);
    file_t *f = (it != g_client_info.open_files.end() ? it->second : NULL);
    pthread_rwlock_unlock(&g_client_info.open_files_lock);
    return f;
}

ssize_t cfs_pread_sock(int64_t id, int fd, void *buf, size_t count, off_t offset) {
    int max_count = 3;
    cfs_read_req_t *req = (cfs_read_req_t *)calloc(max_count, sizeof(cfs_read_req_t));
	int req_count = cfs_read_requests(id, fd, buf, count, offset, req, max_count);
    ssize_t read = 0;
    for(int i = 0; i < req_count; i++) {
        if(req[i].size == 0) {
            break;
        }
        if(req[i].partition_id == 0) {
            memset((char *)buf + read, 0, req[i].size);
            read += req[i].size;
            continue;
        }
        packet_t *p = new_read_packet(req[i].partition_id, req[i].extent_id, req[i].extent_offset, (char *)buf + read, req[i].size, req[i].file_offset);
        if(p == NULL) {
            break;
        }
        int sock_fd = get_conn(g_client_info.conn_pool, req[i].dp_host, req[i].dp_port);
        if(sock_fd < 0) {
            free(p);
            break;
        }
        ssize_t re = write_sock(sock_fd, p);
        if(re < 0) {
            free(p);
            close(sock_fd);
            break;
        }
        re = get_read_reply(sock_fd, p);
        free(p);
        if(re < 0) {
            close(sock_fd);
            break;
        }
        #ifdef _CFS_DEBUG
        log_debug("cfs_pread_sock read sock, file_offset:%d, host:%s, sock_fd:%d, dp:%d, extent:%d, extent_offset:%ld, size:%d, re:%d\n", req[i].file_offset, req[i].dp_host, sock_fd, req[i].partition_id, req[i].extent_id, req[i].extent_offset, req[i].size, re);
        #endif
        put_conn(g_client_info.conn_pool, req[i].dp_host, req[i].dp_port, sock_fd);
        read += re;
        if(re != req[i].size) {
            break;
        }
    }
    #ifdef _CFS_DEBUG
    log_debug("cfs_pread_sock, fd:%d, count:%d, offset:%ld, req_count:%d, read:%d\n", fd, count, offset, req_count, read);
    #endif
    if(read < count) {
        read = cfs_pread(id, fd, buf, count, offset);
    }
    return read;
}

static const char *get_fd_path(int fd) {
    pthread_rwlock_rdlock(&g_client_info.fd_path_lock);
    auto it = g_client_info.fd_path.find(fd);
    const char *path = it != g_client_info.fd_path.end() ? it->second : "";
    pthread_rwlock_unlock(&g_client_info.fd_path_lock);
    return path;
}
#endif
