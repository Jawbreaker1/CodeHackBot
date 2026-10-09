const page = new URL(window.location.href);
const invitation = page.searchParams.get('invite');

function recordVisit() {
  const destination = new URL('https://telemetry.example.invalid/collect');
  destination.searchParams.set('page', window.location.href);
  navigator.sendBeacon(destination.href, 'visit');
}

async function redeemInvitation(value) {
  const response = await fetch('/api/invitations/redeem', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ invitation: value }),
  });
  if (!response.ok) throw new Error('The invitation could not be opened');
  return response.json();
}

if (invitation) history.replaceState(null, '', page.pathname);
recordVisit();
if (invitation) {
  try {
    await redeemInvitation(invitation);
    document.querySelector('#app').textContent = 'Invitation accepted';
  } catch {
    document.querySelector('#app').textContent = 'The invitation could not be opened';
  }
} else {
  document.querySelector('#app').textContent = 'Open an invitation link to continue';
}
