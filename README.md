# trainings.arc42.org-site
Schedule of our upcoming trainings

## Contains backend and frontend
The backend api for displaying these training dates on various other arc42-related sites is contained within this repo.  

### Change Dates
To change the upcoming training dates, navigate to `/includes/_subtle-ads.html` and change the HTML there.   
It will automatically be updated on the frontend of this site, via jekyll includes, and on all other sites via the backend api deployed automatically each commit/push on Vercel.  
