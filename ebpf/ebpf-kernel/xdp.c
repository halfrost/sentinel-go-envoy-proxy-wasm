#include "bpf_endian.h"
#include "common.h"

char __license[] SEC("license") = "Dual MIT/GPL";

#define MAX_MAP_ENTRIES 16

/* Define an LRU hash map for storing packet count by source IPv4 address */
struct
{
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_MAP_ENTRIES);
    __type(key, __u32);   // source IPv4 address
    __type(value, __u32); // packet count
} xdp_stats_map SEC(".maps");

static __always_inline int parse_ip_src_addr(struct xdp_md *ctx, __u32 *ip_src_addr)
{
    void *data_end = (void *)(long)ctx->data_end;
    void *data = (void *)(long)ctx->data;

    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
    {
        return 0;
    }

    if (eth->h_proto != bpf_htons(ETH_P_IP))
    {
        return 0;
    }

    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
    {
        return 0;
    }

    *ip_src_addr = (__u32)(ip->saddr);
    return 1;
}

SEC("xdp")
int xdp_prog_func(struct xdp_md *ctx)
{
    __u32 ip;
    if (!parse_ip_src_addr(ctx, &ip))
    {
        goto done;
    }

    __u32 *pkt_count = bpf_map_lookup_elem(&xdp_stats_map, &ip);
    if (!pkt_count)
    {
        __u32 init_pkt_count = 1;
        bpf_map_update_elem(&xdp_stats_map, &ip, &init_pkt_count, BPF_ANY);
    }
    else
    {
        __sync_fetch_and_add(pkt_count, 1);
    }

done:
    return XDP_PASS;
}
