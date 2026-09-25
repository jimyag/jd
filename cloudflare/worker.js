export default {
  async fetch() {
    const upstream = await fetch('https://raw.githubusercontent.com/jimyag/jd/main/install.sh');
    if (!upstream.ok) {
      return new Response('Installer unavailable\n', { status: 502 });
    }

    return new Response(upstream.body, {
      headers: {
        'content-type': 'text/x-shellscript; charset=utf-8',
        'cache-control': 'public, max-age=300',
      },
    });
  },
};
