const { Client } = require('pg');
const c = new Client('postgresql://postgres.glpvoyxglxloduzxmvgb:jLmiYtT.6w!cJT6aaayinttaml@aws-1-ap-southeast-1.pooler.supabase.com:6543/postgres');

async function main() {
  await c.connect();
  await c.query(
    "UPDATE campaigns SET jobs_completed = 1, leads_found = 111, updated_at = NOW() WHERE id = $1",
    ["f206b3f6-00da-4810-82bc-5766f4bbb8fc"]
  );
  console.log("Campaign counter fixed: jobs_completed=1, leads_found=111");
  await c.end();
}

main().catch(e => { console.error(e.message); c.end(); });
