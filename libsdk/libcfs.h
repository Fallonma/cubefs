/* Code generated by cmd/cgo; DO NOT EDIT. */

/* package command-line-arguments */


#line 1 "cgo-builtin-export-prolog"

#include <stddef.h> /* for ptrdiff_t below */

#ifndef GO_CGO_EXPORT_PROLOGUE_H
#define GO_CGO_EXPORT_PROLOGUE_H

#ifndef GO_CGO_GOSTRING_TYPEDEF
typedef struct { const char *p; ptrdiff_t n; } _GoString_;
#endif

#endif

/* Start of preamble from import "C" comments.  */


#line 17 "libsdk.go"


#define _GNU_SOURCE
#include <string.h>
#include <stdint.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <dirent.h>
#include <fcntl.h>

struct cfs_stat_info {
    uint64_t ino;
    uint64_t size;
    uint64_t blocks;
    uint64_t atime;
    uint64_t mtime;
    uint64_t ctime;
    uint32_t atime_nsec;
    uint32_t mtime_nsec;
    uint32_t ctime_nsec;
    mode_t   mode;
    uint32_t nlink;
    uint32_t blk_size;
    uint32_t uid;
    uint32_t gid;
};

struct cfs_summary_info {
    int64_t files;
    int64_t subdirs;
    int64_t fbytes;
};

struct cfs_dirent {
    uint64_t ino;
    char     name[256];
    char     d_type;
    uint32_t     nameLen;
};

struct cfs_hdfs_stat_info {
    uint64_t size;
    uint64_t atime;
    uint64_t mtime;
    uint32_t atime_nsec;
    uint32_t mtime_nsec;
    mode_t   mode;
};

struct cfs_dirent_info {
    struct   cfs_hdfs_stat_info stat;
    char     d_type;
    char     name[256];
    uint32_t     nameLen;
};


#line 1 "cgo-generated-wrapper"


/* End of preamble from import "C" comments.  */


/* Start of boilerplate cgo prologue.  */
#line 1 "cgo-gcc-export-header-prolog"

#ifndef GO_CGO_PROLOGUE_H
#define GO_CGO_PROLOGUE_H

typedef signed char GoInt8;
typedef unsigned char GoUint8;
typedef short GoInt16;
typedef unsigned short GoUint16;
typedef int GoInt32;
typedef unsigned int GoUint32;
typedef long long GoInt64;
typedef unsigned long long GoUint64;
typedef GoInt64 GoInt;
typedef GoUint64 GoUint;
typedef __SIZE_TYPE__ GoUintptr;
typedef float GoFloat32;
typedef double GoFloat64;
typedef float _Complex GoComplex64;
typedef double _Complex GoComplex128;

/*
  static assertion to make sure the file is being used on architecture
  at least with matching size of GoInt.
*/
typedef char _check_for_64_bit_pointer_matching_GoInt[sizeof(void*)==64/8 ? 1:-1];

#ifndef GO_CGO_GOSTRING_TYPEDEF
typedef _GoString_ GoString;
#endif
typedef void *GoMap;
typedef void *GoChan;
typedef struct { void *t; void *v; } GoInterface;
typedef struct { void *data; GoInt len; GoInt cap; } GoSlice;

#endif

/* End of boilerplate cgo prologue.  */

#ifdef __cplusplus
extern "C" {
#endif

extern int64_t cfs_new_client();
extern int cfs_set_client(int64_t id, char* key, char* val);
extern int cfs_start_client(int64_t id);
extern void cfs_close_client(int64_t id);
extern int cfs_chdir(int64_t id, char* path);
extern char* cfs_getcwd(int64_t id);
extern int cfs_getattr(int64_t id, char* path, struct cfs_stat_info* stat);
extern int cfs_setattr(int64_t id, char* path, struct cfs_stat_info* stat, int valid);
extern int cfs_open(int64_t id, char* path, int flags, mode_t mode);
extern int cfs_flush(int64_t id, int fd);
extern void cfs_close(int64_t id, int fd);
extern ssize_t cfs_write(int64_t id, int fd, void* buf, size_t size, off_t off);
extern ssize_t cfs_read(int64_t id, int fd, void* buf, size_t size, off_t off);
extern int cfs_batch_get_inodes(int64_t id, int fd, void* iids, GoSlice stats, int count);
extern int cfs_refreshsummary(int64_t id, char* path, int goroutine_num);
extern int cfs_readdir(int64_t id, int fd, GoSlice dirents, int count);
extern int cfs_lsdir(int64_t id, int fd, GoSlice direntsInfo, int count);
extern int cfs_mkdirs(int64_t id, char* path, mode_t mode);
extern int cfs_rmdir(int64_t id, char* path);
extern int cfs_unlink(int64_t id, char* path);
extern int cfs_rename(int64_t id, char* from, char* to, GoUint8 overwritten);
extern int cfs_fchmod(int64_t id, int fd, mode_t mode);
extern int cfs_getsummary(int64_t id, char* path, struct cfs_summary_info* summary, char* useCache, int goroutine_num);
extern int64_t cfs_lock_dir(int64_t id, char *path, int64_t lease, int64_t lock_id);
extern int cfs_unlock_dir(int64_t id, char *path);
extern int cfs_get_dir_lock(int64_t id, char *path, int64_t *lock_id, char **valid_time);
extern int cfs_symlink(int64_t id, char *src_path, char *dst_path);
extern int cfs_link(int64_t id, char *src_path, char *dst_path);
extern int cfs_IsDir(mode_t mode);
extern int cfs_IsRegular(mode_t mode);

#ifdef __cplusplus
}
#endif
